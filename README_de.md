# Gmail-zu-Markdown Exporter & Google Drive Sync CLI

Dieses Projekt besteht aus zwei aufeinander abgestimmten Komponenten, um E-Mails aus Gmail automatisch als Markdown-Notizen (inkl. YAML-Frontmatter und Anhängen) über Google Drive in ein lokales Verzeichnis (z. B. ein Obsidian-Vault) zu übertragen:

1. **[`exportGmailToMarkdown.js`](./exportGmailToMarkdown.js)** – Ein **Google Apps Script**, das markierte E-Mails in Gmail erkennt, in Markdown mit YAML-Frontmatter umwandelt und samt Anhängen in Google Drive ablegt.
2. **[`gdrive-sync` (`main.go`)](./main.go)** – Ein **Go (Golang v1.27) CLI-Programm**, das die exportierten Markdown-Dateien und deren Anhang-Unterordner aus Google Drive in einen lokalen Ordner herunterlädt und nach erfolgreichem Download in Google Drive aufräumt.

---

<!-- START doctoc -->
<!-- END doctoc -->

---

## Inhaltsverzeichnis

1. [Teil 1: Google Apps Script (`exportGmailToMarkdown.js`)](#1-google-apps-script-exportgmailtomarkdownjs)
2. [Teil 2: Go CLI-Programm (`gdrive-sync`)](#2-go-cli-programm-gdrive-sync)
3. [Teil 3: Google OAuth2 Client-Credentials & Service-Account erstellen](#3-google-oauth2-client-credentials--service-account-erstellen)

---

## 1. Google Apps Script (`exportGmailToMarkdown.js`)

Das Skript durchsucht das Gmail-Postfach nach noch nicht verarbeiteten E-Mails, deren Betreff am Ende einen Kategorie-Marker enthält, und speichert diese als `.md`-Dateien in Google Drive.

### Betreff-Syntax

| Marker am Betreff-Ende | Zielordner in Google Drive       | Beschreibung                                                |
| :--------------------- | :------------------------------- | :---------------------------------------------------------- |
| `@snippet`             | `Gmail_Snippets/Snippets`        | Code-Snippets & kurze Notizen                               |
| `@tech`                | `Gmail_Snippets/Tech`            | Technische Artikel, Dokumentation & Know-how                |
| `@priv`                | `Gmail_Snippets/Private`         | Private Notizen & Dokumente                                 |
| `#<tag>`               | YAML-Frontmatter `tags: [<tag>]` | Beliebige Tags (auch mehrere möglich, z. B. `#golang #cli`) |

#### Beispiele für E-Mail-Betreffzeilen:

- `Docker Container aufräumen #docker #linux @snippet`
  - **Titel / Dateiname:** `Docker Container aufräumen.md`
  - **Zielordner:** `Gmail_Snippets/Snippets`
  - **Frontmatter:** `tags: [docker, linux]`
- `Kubernetes Netzwerk-Architektur #k8s @tech`
  - **Titel / Dateiname:** `Kubernetes Netzwerk-Architektur.md`
  - **Zielordner:** `Gmail_Snippets/Tech`
  - **Frontmatter:** `tags: [k8s]`
- `Steuerunterlagen Checkliste @priv`
  - **Titel / Dateiname:** `Steuerunterlagen Checkliste.md`
  - **Zielordner:** `Gmail_Snippets/Private`
  - **Frontmatter:** `tags: []`

### Umgang mit E-Mail-Anhängen

Enthält eine E-Mail Dateianhänge (Bilder, PDFs, Archive etc.):

1. Im jeweiligen Kategorie-Ordner (`Snippets`, `Tech` oder `Private`) wird automatisch ein Unterordner mit dem Namen der Markdown-Datei angelegt: `<markdown-dateiname>/`.
2. Alle Anhänge werden in diesem Unterordner gespeichert.
3. Am Ende der erzeugten Markdown-Datei wird automatisch ein Abschnitt `## Anhänge` angefügt:
   - **Bilder (`image/*`)** werden als Markdown-Bild eingebettet: `![bild.png](<Ordner>/<bild.png>)`
   - **Sonstige Dateien** werden als relativer Link referenziert: `[dokument.pdf](<Ordner>/<dokument.pdf>)`

### Erzeugtes Markdown-Format

```markdown
---
type: Note
title: Docker Container aufräumen
description: "Kurze Zusammenfassung aus der ersten Zeile der E-Mail"
status: ""
notebook: ""
tags: [docker, linux]
uses: []
depends_on: []
contains: []
references: []
adr:
  -
relation: []
api: ""
creation date: 2026-11-02 09:30
modification date: Montag 02. November 2026 09:30:15
---

Hier steht der eigentliche Textkörper der E-Mail...

## Anhänge

![screenshot.png](Docker%20Container%20aufr%C3%A4umen/screenshot.png)
[anleitung.pdf](Docker%20Container%20aufr%C3%A4umen/anleitung.pdf)
```

### Einrichtung in Google Apps Script

1. Öffne [script.google.com](https://script.google.com/) und erstelle ein **Neues Projekt**.
2. Kopiere den gesamten Inhalt aus [`exportGmailToMarkdown.js`](./exportGmailToMarkdown.js) in den Editor (`Code.gs`).
3. Klicke oben auf **Ausführen** (`exportGmailToMarkdown`), um beim ersten Mal die Berechtigungen für **Gmail** und **Google Drive** zu erteilen.
4. _(Optional)_ Richte links unter **Trigger (Wecker-Symbol)** $\rightarrow$ **Trigger hinzufügen** eine zeitgesteuerte Ausführung ein (z. B. _Zeitgesteuert_ $\rightarrow$ _Minütlich_ oder _Alle 15 Minuten_), damit neue E-Mails vollautomatisch exportiert werden.
5. Verarbeitete E-Mails erhalten in Gmail automatisch das Label `Exportiert`, sodass sie nicht doppelt exportiert werden.

---

## 2. Go CLI-Programm (`gdrive-sync`)

Das Go-Programm fragt einen angegebenen Google Drive Ordner ab, lädt alle darin enthaltenen Markdown-Dateien samt den zugehörigen Anhang-Unterordnern (`<markdown-dateiname>/`) in ein lokales Verzeichnis herunter und löscht die Dateien und Anhänge anschließend in Google Drive.

### Sicherheitsmechanismen

- **Atomarer Download:** Dateien werden zunächst in eine temporäre `.part`-Datei geschrieben, per `fsync` auf die Festplatte geschrieben und erst nach vollständigem Download umbenannt.
- **Löschen nur bei Erfolg:** Eine Markdown-Datei wird in Google Drive **nur dann** gelöscht, wenn sowohl die `.md`-Datei selbst als auch **alle** Dateien im zugehörigen Anhang-Unterordner `<markdown-dateiname>/` fehlerfrei heruntergeladen wurden.
- **Erhalt der Ordnerstruktur:** Die Basis- und Kategorie-Ordner (`Gmail_Snippets`, `Snippets`, `Tech`, `Private`) sind geschützt und bleiben in Google Drive für zukünftige Exporte bestehen; gelöscht werden nur die heruntergeladenen `.md`-Dateien sowie deren Anhang-Unterordner.

### Kompilierung

Voraussetzung ist **Go 1.27** (oder neuer):

```bash
go mod tidy
go build -o gdrive-sync .
```

### Kommandozeilen-Parameter

| Parameter       | Kurzform | Standardwert       | Beschreibung                                                                                   |
| :-------------- | :------- | :----------------- | :--------------------------------------------------------------------------------------------- |
| `-drive-folder` | `-d`     | _(erforderlich)_   | Pfad des Google Drive Ordners (z. B. `"Gmail_Snippets/Snippets"`) oder direkte Drive Folder-ID |
| `-local-dir`    | `-o`     | _(erforderlich)_   | Lokaler Zielordner zum Speichern der heruntergeladenen Dateien und Anhänge                     |
| `-credentials`  | –        | `credentials.json` | Pfad zur Google OAuth2 Client-Credentials- oder Service-Account-JSON-Datei                     |
| `-token`        | –        | `token.json`       | Pfad zur lokal zwischengespeicherten OAuth2-Token-Datei                                        |
| `-trash`        | –        | `false`            | Verschiebt Dateien in Google Drive in den Papierkorb, anstatt sie endgültig zu löschen         |
| `-dry-run`      | –        | `false`            | Simulationsmodus: zeigt nur an, was heruntergeladen und gelöscht würde                         |

### Anwendungsbeispiele

#### Beispiel 1: Einzelnen Kategorie-Ordner (`Tech`) abrufen und lokal speichern

Lädt alle Markdown-Dateien und Anhänge aus `Gmail_Snippets/Tech` in den lokalen Ordner `./notizen/tech` herunter und löscht sie danach in Google Drive:

```bash
./gdrive-sync \
  -drive-folder "Gmail_Snippets/Tech" \
  -local-dir "./notizen/tech"
```

**Beispiel-Ausgabe:**

```text
2026/11/02 09:55:01 Starte Synchronisation von Google Drive Ordner "Tech" (ID: 1a2b3c...) -> lokal "./notizen/tech"
2026/11/02 09:55:02 Heruntergeladen: "Kubernetes Netzwerk-Architektur.md" (1420 Bytes) -> notizen/tech/Kubernetes Netzwerk-Architektur.md
2026/11/02 09:55:03 Heruntergeladen: "architektur.png" (84312 Bytes) -> notizen/tech/Kubernetes Netzwerk-Architektur/architektur.png
2026/11/02 09:55:03 In Google Drive gelöscht: "architektur.png" (ID: 9z8y7x...)
2026/11/02 09:55:04 In Google Drive gelöscht: "Kubernetes Netzwerk-Architektur" (ID: 5m4n3b...)
2026/11/02 09:55:04 In Google Drive gelöscht: "Kubernetes Netzwerk-Architektur.md" (ID: 2k3j4h...)
2026/11/02 09:55:04 Fertig! Alle Dateien und Anhänge wurden erfolgreich verarbeitet.
```

#### Beispiel 2: Gesamten `Gmail_Snippets`-Ordner (inkl. `Snippets`, `Tech`, `Private`) synchronisieren

```bash
./gdrive-sync -d "Gmail_Snippets" -o "/pfad/zum/obsidian-vault/Inbox"
```

#### Beispiel 3: Trockenlauf (`-dry-run`) und Papierkorb-Modus (`-trash`)

```bash
# Prüfen, welche Dateien betroffen wären (ohne Download/Löschung):
./gdrive-sync -d "Gmail_Snippets/Snippets" -o "./snippets" -dry-run

# Dateien nach dem Download in den Google Drive Papierkorb verschieben statt endgültig zu löschen:
./gdrive-sync -d "Gmail_Snippets/Snippets" -o "./snippets" -trash
```

---

## 3. Google OAuth2 Client-Credentials & Service-Account erstellen

Damit `gdrive-sync` auf Google Drive zugreifen kann, wird eine `credentials.json`-Datei aus der [Google Cloud Console](https://console.cloud.google.com/) benötigt. Das Programm erkennt automatisch, ob es sich um **OAuth2 Desktop-Credentials** oder um einen **Service-Account** handelt.

### Vorbereitung (für beide Varianten erforderlich)

1. Öffne die [Google Cloud Console](https://console.cloud.google.com/).
2. Erstelle oben über die Projektauswahl ein **Neues Projekt** (z. B. `gdrive-markdown-sync`) oder wähle ein bestehendes Projekt aus.
3. Navigiere im Menü zu **APIs & Dienste** $\rightarrow$ **Bibliothek** (Library).
4. Suche nach **Google Drive API**, wähle sie aus und klicke auf **Aktivieren** (Enable).

---

### Variante A: OAuth 2.0 Client-Credentials (Empfohlen für persönliches Google Drive)

Diese Variante eignet sich am besten, wenn das Google Apps Script unter deinem persönlichen Google-Konto läuft.

#### 1. OAuth-Zustimmungsbildschirm (Consent Screen) konfigurieren

1. Gehe zu **APIs & Dienste** $\rightarrow$ **OAuth-Zustimmungsbildschirm** (bzw. _Google Auth Platform_ $\rightarrow$ _Branding / Audience_).
2. Wähle als Nutzertyp **Extern** (oder _Intern_ bei Google Workspace) und klicke auf **Erstellen**.
3. Vergib einen Anwendungsnamen (z. B. `GDrive Sync CLI`) und trage deine E-Mail-Adresse als Support- und Entwicklerkontakt ein.
4. Füge unter **Bereiche (Scopes)** den Bereich `https://www.googleapis.com/auth/drive` hinzu.
5. Trage unter **Testnutzer (Test users)** deine eigene **Gmail-Adresse** ein (solange sich die App im Status „Test“ befindet, dürfen sich nur eingetragene Testnutzer anmelden).

#### 2. OAuth-Client-ID erstellen & `credentials.json` herunterladen

1. Navigiere zu **APIs & Dienste** $\rightarrow$ **Anmeldedaten** (Credentials).
2. Klicke oben auf **+ Anmeldedaten erstellen** $\rightarrow$ **OAuth-Client-ID**.
3. Wähle als Anwendungstyp **Desktop-App** (Desktop app) und vergib einen Namen (z. B. `gdrive-sync-cli`).
4. Klicke auf **Erstellen** und anschließend im Dialog auf **JSON herunterladen**.
5. Speichere die heruntergeladene Datei als `credentials.json` im Programmverzeichnis (neben `gdrive-sync`).

#### 3. Einmalige Anmeldung beim ersten Start

Beim ersten Aufruf von `./gdrive-sync` gibt das Programm einen Link im Terminal aus:

1. Öffne den angezeigten Link im Browser und melde dich mit deinem Google-Konto an.
2. Bestätige den Zugriff (bei eigenem Testprojekt über _„Erweitert“ $\rightarrow$ „Weiter zu ... (unsicher)“_).
3. Das CLI-Programm empfängt den Code automatisch über einen kurzzeitig geöffneten lokalen Callback-Port (`127.0.0.1`) und speichert das Refresh-Token in `token.json`. Alle zukünftigen Aufrufe laufen danach vollautomatisch ohne Browser-Interaktion.

---

### Variante B: Service-Account JSON-Datei (Für Server / Headless-Betrieb ohne Browser)

Ein Service-Account besitzt eine eigene technische E-Mail-Adresse (`...@<projekt>.iam.gserviceaccount.com`) und benötigt keinen Browser-Login.

#### 1. Service-Account anlegen & JSON-Schlüssel herunterladen

1. Navigiere in der Google Cloud Console zu **APIs & Dienste** $\rightarrow$ **Anmeldedaten** (oder **IAM & Verwaltung** $\rightarrow$ **Dienstkonten**).
2. Klicke auf **+ Anmeldedaten erstellen** $\rightarrow$ **Dienstkonto** (Service account).
3. Vergib einen Namen (z. B. `gdrive-sync-bot`) und klicke auf **Erstellen und fortfahren** $\rightarrow$ **Fertig**.
4. Klicke in der Liste auf das neu erstellte Dienstkonto und wechsle oben zum Reiter **Schlüssel** (Keys).
5. Klicke auf **Schlüssel hinzufügen** $\rightarrow$ **Neuen Schlüssel erstellen**, wähle **JSON** und klicke auf **Erstellen**.
6. Speichere die heruntergeladene JSON-Datei als `credentials.json` im Programmverzeichnis (oder übergib sie per `-credentials /pfad/zur/service-account.json`).

#### 2. Wichtig: Google Drive Ordner für den Service-Account freigeben

Da ein Service-Account ein eigenes Konto ist, sieht er deine persönlichen Drive-Ordner standardmäßig nicht:

1. Kopiere die E-Mail-Adresse des Service-Accounts (aus der JSON-Datei im Feld `"client_email"`).
2. Öffne [Google Drive](https://drive.google.com/), klicke mit der rechten Maustaste auf den Ordner **`Gmail_Snippets`** $\rightarrow$ **Freigeben** (Share).
3. Füge die E-Mail-Adresse des Service-Accounts mit der Berechtigung **Mitbearbeiter (Editor)** hinzu (damit er Dateien herunterladen und anschließend löschen darf).

## Links

[md_indexer-searcher](https://github.com/Zheng-Bote/md_indexer-searcher)
[convert_to_markdown](https://github.com/Zheng-Bote/convert_to_markdown)
