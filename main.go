package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const folderMimeType = "application/vnd.google-apps.folder"

// Geschützte Kategorie-Ordner, die als Struktur in Google Drive erhalten bleiben sollen
var protectedCategoryFolders = map[string]bool{
	"Gmail_Snippets": true,
	"Snippets":       true,
	"Tech":           true,
	"Private":        true,
}

type Config struct {
	DriveFolder     string
	LocalDir        string
	CredentialsFile string
	TokenFile       string
	UseTrash        bool
	DryRun          bool
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.DriveFolder, "drive-folder", "", "Google Drive Ordnerpfad (z.B. \"Gmail_Snippets/Snippets\") oder Ordner-ID (erforderlich)")
	flag.StringVar(&cfg.DriveFolder, "d", "", "Kurzform für -drive-folder")
	flag.StringVar(&cfg.LocalDir, "local-dir", "", "Lokaler Zielordner zum Speichern der Dateien und Anhänge (erforderlich)")
	flag.StringVar(&cfg.LocalDir, "o", "", "Kurzform für -local-dir")
	flag.StringVar(&cfg.CredentialsFile, "credentials", "credentials.json", "Pfad zur Google OAuth2 Client-Credentials oder Service-Account JSON-Datei")
	flag.StringVar(&cfg.TokenFile, "token", "token.json", "Pfad zur gespeicherten OAuth2-Token-Datei")
	flag.BoolVar(&cfg.UseTrash, "trash", false, "Dateien im Google Drive in den Papierkorb verschieben statt endgültig zu löschen")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Simulationsmodus: zeigt nur an, was heruntergeladen und gelöscht würde")
	flag.Parse()

	if strings.TrimSpace(cfg.DriveFolder) == "" || strings.TrimSpace(cfg.LocalDir) == "" {
		fmt.Fprintf(os.Stderr, "Fehler: -drive-folder und -local-dir müssen angegeben werden.\n\n")
		fmt.Fprintf(os.Stderr, "Verwendung:\n  %s -drive-folder \"Gmail_Snippets/Snippets\" -local-dir \"./downloads\"\n\nParameter:\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx := context.Background()

	srv, err := newDriveService(ctx, cfg.CredentialsFile, cfg.TokenFile)
	if err != nil {
		log.Fatalf("Fehler bei der Google Drive Authentifizierung: %v", err)
	}

	rootFolder, err := resolveDriveFolder(ctx, srv, cfg.DriveFolder)
	if err != nil {
		log.Fatalf("Google Drive Ordner %q konnte nicht gefunden werden: %v", cfg.DriveFolder, err)
	}

	log.Printf("Starte Synchronisation von Google Drive Ordner %q (ID: %s) -> lokal %q", rootFolder.Name, rootFolder.Id, cfg.LocalDir)

	if err := os.MkdirAll(cfg.LocalDir, 0o755); err != nil {
		log.Fatalf("Lokaler Zielordner %q konnte nicht erstellt werden: %v", cfg.LocalDir, err)
	}

	if err := processFolder(ctx, srv, rootFolder.Id, cfg.LocalDir, cfg); err != nil {
		log.Fatalf("Synchronisation mit Fehlern beendet: %v", err)
	}

	log.Println("Fertig! Alle Dateien und Anhänge wurden erfolgreich verarbeitet.")
}

// newDriveService initialisiert den Google Drive API v3 Client (unterstützt OAuth2 Desktop-Credentials, Service-Accounts & ADC).
func newDriveService(ctx context.Context, credentialsPath, tokenPath string) (*drive.Service, error) {
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Fallback auf Application Default Credentials (z.B. gcloud auth application-default login)
			log.Printf("Hinweis: %q nicht gefunden, verwende Application Default Credentials...", credentialsPath)
			return drive.NewService(ctx, option.WithScopes(drive.DriveScope))
		}
		return nil, fmt.Errorf("Credentials-Datei %q konnte nicht gelesen werden: %w", credentialsPath, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		if credType, ok := raw["type"].(string); ok && credType == "service_account" {
			creds, err := google.CredentialsFromJSON(ctx, data, drive.DriveScope)
			if err != nil {
				return nil, fmt.Errorf("Service-Account-Credentials ungültig: %w", err)
			}
			return drive.NewService(ctx, option.WithCredentials(creds))
		}
	}

	config, err := google.ConfigFromJSON(data, drive.DriveScope)
	if err != nil {
		return nil, fmt.Errorf("OAuth2-Konfiguration aus %q konnte nicht geladen werden: %w", credentialsPath, err)
	}

	client, err := getOAuthClient(ctx, config, tokenPath)
	if err != nil {
		return nil, err
	}

	return drive.NewService(ctx, option.WithHTTPClient(client))
}

func getOAuthClient(ctx context.Context, config *oauth2.Config, tokenPath string) (*http.Client, error) {
	tok, err := tokenFromFile(tokenPath)
	if err != nil {
		tok, err = getTokenFromWeb(ctx, config)
		if err != nil {
			return nil, err
		}
		if err := saveToken(tokenPath, tok); err != nil {
			log.Printf("Warnung: Token konnte nicht in %q gespeichert werden: %v", tokenPath, err)
		}
	}
	return config.Client(ctx, tok), nil
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func getTokenFromWeb(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Starte lokalen Callback-Server für den OAuth2-Redirect
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("lokaler OAuth-Callback-Listener konnte nicht gestartet werden: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			http.Error(w, "Authentifizierung fehlgeschlagen: "+errMsg, http.StatusBadRequest)
			errCh <- fmt.Errorf("OAuth-Fehler: %s", errMsg)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Kein Autorisierungscode erhalten", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Authentifizierung erfolgreich! Du kannst dieses Browserfenster jetzt schließen.")
		codeCh <- code
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("\nBitte öffne folgenden Link im Browser, um den Google Drive Zugriff zu autorisieren:\n\n%s\n\nWarte auf Autorisierung...\n", authURL)

	select {
	case code := <-codeCh:
		return config.Exchange(ctx, code)
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func saveToken(path string, token *oauth2.Token) error {
	fmt.Printf("Speichere OAuth-Token in %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(token)
}

// resolveDriveFolder löst einen Pfad wie "Gmail_Snippets/Snippets" oder eine direkte Ordner-ID auf.
func resolveDriveFolder(ctx context.Context, srv *drive.Service, folderSpec string) (*drive.File, error) {
	trimmed := strings.Trim(folderSpec, "/")
	parts := strings.Split(trimmed, "/")

	// Falls nur ein Segment übergeben wurde, prüfen wir zuerst nach Name und ansonsten als direkte Ordner-ID
	var currentParentID string
	var currentFolder *drive.File

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		q := fmt.Sprintf("mimeType = '%s' and name = '%s' and trashed = false", folderMimeType, escapeDriveQuery(part))
		if currentParentID != "" {
			q += fmt.Sprintf(" and '%s' in parents", escapeDriveQuery(currentParentID))
		}

		res, err := srv.Files.List().
			Context(ctx).
			Q(q).
			Fields("files(id, name, mimeType)").
			PageSize(10).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Do()
		if err != nil {
			return nil, fmt.Errorf("Ordnersuche für %q fehlgeschlagen: %w", part, err)
		}

		if len(res.Files) == 0 {
			// Falls es das einzige Segment ist, prüfen ob es direkt eine Google Drive Folder-ID ist
			if len(parts) == 1 && i == 0 {
				f, idErr := srv.Files.Get(part).
					Context(ctx).
					Fields("id, name, mimeType").
					SupportsAllDrives(true).
					Do()
				if idErr == nil && f.MimeType == folderMimeType {
					return f, nil
				}
			}
			return nil, fmt.Errorf("Unterordner %q wurde in Google Drive nicht gefunden", part)
		}

		currentFolder = res.Files[0]
		currentParentID = currentFolder.Id
	}

	if currentFolder == nil {
		return nil, errors.New("ungültige Ordnerangabe")
	}
	return currentFolder, nil
}

// processFolder lädt alle Dateien und die zugehörigen Anhang-Unterordner herunter und löscht sie nach Erfolg in Google Drive.
func processFolder(ctx context.Context, srv *drive.Service, folderID, localDir string, cfg Config) error {
	items, err := listFolderChildren(ctx, srv, folderID)
	if err != nil {
		return err
	}

	var files []*drive.File
	subfoldersByName := make(map[string]*drive.File)
	processedSubfolders := make(map[string]bool)

	for _, item := range items {
		if item.MimeType == folderMimeType {
			subfoldersByName[item.Name] = item
		} else {
			files = append(files, item)
		}
	}

	var errs []error

	// 1. Alle Dateien (z.B. "<title>.md") inkl. passendem Anhang-Unterordner "<title>/" verarbeiten
	for _, file := range files {
		localFilePath := filepath.Join(localDir, sanitizeFileName(file.Name))
		baseName := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
		attFolder, hasAttFolder := subfoldersByName[baseName]

		if err := downloadFile(ctx, srv, file, localFilePath, cfg.DryRun); err != nil {
			log.Printf("FEHLER beim Download von %q: %v (wird NICHT in Google Drive gelöscht)", file.Name, err)
			errs = append(errs, err)
			if hasAttFolder {
				processedSubfolders[attFolder.Id] = true
			}
			continue
		}

		// Falls ein zugehöriger Anhang-Unterordner "<markdown-dateiname>/" existiert, diesen herunterladen & löschen
		if hasAttFolder {
			processedSubfolders[attFolder.Id] = true
			localAttDir := filepath.Join(localDir, sanitizeFileName(attFolder.Name))

			if err := downloadAndDeleteAttachmentFolder(ctx, srv, attFolder, localAttDir, cfg); err != nil {
				log.Printf("FEHLER bei Anhängen in %q: %v (Hauptdatei %q wird daher NICHT in Google Drive gelöscht)", attFolder.Name, err, file.Name)
				errs = append(errs, err)
				continue
			}
		}

		// Nach erfolgreichem Download (inkl. Anhängen) die Hauptdatei in Google Drive löschen
		if err := deleteDriveItem(ctx, srv, file, cfg); err != nil {
			log.Printf("FEHLER beim Löschen von %q in Google Drive: %v", file.Name, err)
			errs = append(errs, err)
		}
	}

	// 2. Übrige Unterordner verarbeiten (z.B. Kategorie-Ordner "Snippets", "Tech", "Private" oder alleinstehende Anhang-Ordner)
	for _, subfolder := range subfoldersByName {
		if processedSubfolders[subfolder.Id] {
			continue
		}

		localSubDir := filepath.Join(localDir, sanitizeFileName(subfolder.Name))
		if protectedCategoryFolders[subfolder.Name] {
			if err := os.MkdirAll(localSubDir, 0o755); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := processFolder(ctx, srv, subfolder.Id, localSubDir, cfg); err != nil {
				errs = append(errs, err)
			}
		} else {
			if err := downloadAndDeleteAttachmentFolder(ctx, srv, subfolder, localSubDir, cfg); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

// downloadAndDeleteAttachmentFolder lädt alle Anhänge eines Unterordners herunter und löscht danach die Anhänge sowie den Unterordner in Drive.
func downloadAndDeleteAttachmentFolder(ctx context.Context, srv *drive.Service, folder *drive.File, localDir string, cfg Config) error {
	children, err := listFolderChildren(ctx, srv, folder.Id)
	if err != nil {
		return fmt.Errorf("Inhalt von Anhang-Ordner %q konnte nicht geladen werden: %w", folder.Name, err)
	}

	if !cfg.DryRun {
		if err := os.MkdirAll(localDir, 0o755); err != nil {
			return fmt.Errorf("lokaler Anhang-Ordner %q konnte nicht erstellt werden: %w", localDir, err)
		}
	}

	for _, child := range children {
		if child.MimeType == folderMimeType {
			subLocalDir := filepath.Join(localDir, sanitizeFileName(child.Name))
			if err := downloadAndDeleteAttachmentFolder(ctx, srv, child, subLocalDir, cfg); err != nil {
				return err
			}
			continue
		}

		targetPath := filepath.Join(localDir, sanitizeFileName(child.Name))
		if err := downloadFile(ctx, srv, child, targetPath, cfg.DryRun); err != nil {
			return fmt.Errorf("Anhang %q konnte nicht heruntergeladen werden: %w", child.Name, err)
		}

		if err := deleteDriveItem(ctx, srv, child, cfg); err != nil {
			return fmt.Errorf("Anhang %q konnte nach Download nicht gelöscht werden: %w", child.Name, err)
		}
	}

	// Wenn alle Anhänge erfolgreich heruntergeladen und gelöscht wurden, den leeren Anhang-Unterordner selbst löschen
	if err := deleteDriveItem(ctx, srv, folder, cfg); err != nil {
		return fmt.Errorf("Anhang-Unterordner %q konnte nicht gelöscht werden: %w", folder.Name, err)
	}

	return nil
}

// listFolderChildren gibt alle nicht gelöschten Dateien und Ordner innerhalb eines Google-Drive-Ordners zurück.
func listFolderChildren(ctx context.Context, srv *drive.Service, folderID string) ([]*drive.File, error) {
	var all []*drive.File
	q := fmt.Sprintf("'%s' in parents and trashed = false", escapeDriveQuery(folderID))

	pageToken := ""
	for {
		call := srv.Files.List().
			Context(ctx).
			Q(q).
			Fields("nextPageToken, files(id, name, mimeType, size)").
			PageSize(100).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		res, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("Dateiliste für Ordner %s fehlgeschlagen: %w", folderID, err)
		}

		all = append(all, res.Files...)
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}

	return all, nil
}

// downloadFile lädt eine Datei aus Google Drive atomar (über eine .part-Datei) in den lokalen Zielpfad herunter.
func downloadFile(ctx context.Context, srv *drive.Service, file *drive.File, destPath string, dryRun bool) error {
	if dryRun {
		log.Printf("[DRY-RUN] Würde herunterladen: %q -> %s", file.Name, destPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	var resp *http.Response
	var err error

	// Google Docs/Sheets/Slides Dokumente müssen exportiert werden, normale Dateien (wie .md, Bilder, PDFs) werden direkt geladen
	if strings.HasPrefix(file.MimeType, "application/vnd.google-apps.") {
		exportMime := "text/plain"
		if file.MimeType == "application/vnd.google-apps.document" {
			exportMime = "text/markdown"
		}
		resp, err = srv.Files.Export(file.Id, exportMime).Context(ctx).Download()
	} else {
		resp, err = srv.Files.Get(file.Id).Context(ctx).SupportsAllDrives(true).Download()
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unerwarteter HTTP-Status beim Download: %s", resp.Status)
	}

	tmpPath := destPath + ".part"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	written, copyErr := io.Copy(out, resp.Body)
	syncErr := out.Sync()
	closeErr := out.Close()

	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(tmpPath)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	log.Printf("Heruntergeladen: %q (%d Bytes) -> %s", file.Name, written, destPath)
	return nil
}

// deleteDriveItem löscht eine Datei oder einen Ordner in Google Drive (oder verschiebt sie bei -trash in den Papierkorb).
func deleteDriveItem(ctx context.Context, srv *drive.Service, file *drive.File, cfg Config) error {
	if cfg.DryRun {
		log.Printf("[DRY-RUN] Würde in Google Drive löschen: %q (ID: %s)", file.Name, file.Id)
		return nil
	}

	if cfg.UseTrash {
		_, err := srv.Files.Update(file.Id, &drive.File{Trashed: true}).
			Context(ctx).
			SupportsAllDrives(true).
			Do()
		if err != nil {
			return err
		}
		log.Printf("In Google Drive Papierkorb verschoben: %q (ID: %s)", file.Name, file.Id)
		return nil
	}

	if err := srv.Files.Delete(file.Id).Context(ctx).SupportsAllDrives(true).Do(); err != nil {
		return err
	}
	log.Printf("In Google Drive gelöscht: %q (ID: %s)", file.Name, file.Id)
	return nil
}

func escapeDriveQuery(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

func sanitizeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return "unbenannt"
	}
	return name
}
