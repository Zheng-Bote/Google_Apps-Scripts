function exportGmailToMarkdown() {
  var baseFolderName = "Gmail_Snippets";  // Hauptordner in Google Drive
  var processedLabelName = "Exportiert";  // Verhindert doppelte Exporte

  // Zuordnung von @-Marker zu Unterordnern unterhalb von "Gmail_Snippets"
  var categoryMap = {
    "snippet": "Snippets",
    "tech": "Tech",
    "priv": "Private"
  };

  // Sucht nach E-Mails mit @snippet, @tech oder @priv im Betreff, die noch nicht exportiert wurden
  var searchQuery = 'subject:("@snippet" OR "@tech" OR "@priv") -label:' + processedLabelName;

  var threads = GmailApp.search(searchQuery, 0, 50);
  if (threads.length === 0) return;

  // Hauptordner "Gmail_Snippets" in Google Drive holen oder erstellen
  var baseFolders = DriveApp.getFoldersByName(baseFolderName);
  var baseFolder = baseFolders.hasNext() ? baseFolders.next() : DriveApp.createFolder(baseFolderName);
  var subFolderCache = {};

  // Gmail-Label holen oder erstellen
  var label = GmailApp.getUserLabelByName(processedLabelName);
  if (!label) {
    label = GmailApp.createLabel(processedLabelName);
  }

  var timeZone = Session.getScriptTimeZone();

  for (var i = 0; i < threads.length; i++) {
    var messages = threads[i].getMessages();
    var threadExported = false;

    for (var j = 0; j < messages.length; j++) {
      var message = messages[j];
      var rawSubject = (message.getSubject() || "").trim();

      // Prüft, ob der Betreff am Ende "@snippet", "@tech" oder "@priv" (ggf. mit #<tag>) enthält
      var categoryMatch = rawSubject.match(/@(snippet|tech|priv)(?:\s+#[^\s#@]+)*\s*$/i);
      if (!categoryMatch) continue;

      var categoryKey = categoryMatch[1].toLowerCase();
      var targetSubFolderName = categoryMap[categoryKey];

      // Alle #<tag> aus dem Betreff extrahieren
      var tags = [];
      var tagRegex = /#([^\s#@]+)/g;
      var tagMatch;
      while ((tagMatch = tagRegex.exec(rawSubject)) !== null) {
        tags.push(tagMatch[1]);
      }

      // Bereinigten Titel erstellen (ohne @snippet/@tech/@priv und #<tag>)
      var title = rawSubject
        .replace(/@(snippet|tech|priv)\b/gi, "")
        .replace(/#[^\s#@]+/g, "")
        .replace(/\s+/g, " ")
        .trim();
      if (!title) title = "Ohne Titel";

      var msgDate = message.getDate();

      // Erste nicht-leere Zeile des E-Mail-Texts als Kurzbeschreibung (optional)
      var body = message.getPlainBody().trim();
      var firstLine = (body.split(/\r?\n/).find(function(line) { return line.trim().length > 0; }) || "")
                        .replace(/"/g, '\\"')
                        .trim();

      // Datumsformate wie im Beispiel:
      // creation date: 2026-10-25 10:22
      var creationDate = Utilities.formatDate(msgDate, timeZone, "yyyy-MM-dd HH:mm");
      // modification date: Sonntag 25. Oktober 2026 10:22:48
      var modificationDate = formatGermanLongDate(msgDate, timeZone);

      var tagsFrontmatter = "tags: [" + tags.join(", ") + "]";

      // YAML Frontmatter + Markdown Content zusammenbauen
      var markdownContent = [
        "---",
        "type: Note",
        "title: " + title,
        'description: "' + firstLine + '"',
        'status: ""',
        'notebook: ""',
        tagsFrontmatter,
        "uses: []",
        "depends_on: []",
        "contains: []",
        "references: []",
        "adr:",
        "  - ",
        "relation: []",
        'api: ""',
        "creation date: " + creationDate,
        "modification date: " + modificationDate,
        "---",
        "",
        body
      ].join("\n");

      // Sauberer Dateiname
      var safeTitle = title.replace(/[^a-zA-Z0-9äöüÄÖÜß _-]/g, "").trim() || "Ohne_Titel";
      var fileName = safeTitle + ".md";

      // Ziel-Unterordner "Gmail_Snippets/[Snippets|Tech|Private]" holen oder erstellen
      var targetFolder = getOrCreateSubfolder(baseFolder, targetSubFolderName, subFolderCache);

      // E-Mail-Anhänge prüfen und ggf. in Unterordner "<safeTitle>/" speichern
      var attachments = message.getAttachments();
      var attachmentLinks = [];

      if (attachments.length > 0) {
        var attachmentFolder = getOrCreateSubfolder(targetFolder, safeTitle);
        var encodedFolder = encodeURIComponent(safeTitle);

        for (var k = 0; k < attachments.length; k++) {
          var attachment = attachments[k];
          var savedFile = attachmentFolder.createFile(attachment.copyBlob());
          var attName = savedFile.getName();
          var encodedFile = encodeURIComponent(attName);
          var contentType = attachment.getContentType() || "";

          // Bilder als Markdown-Bild (![...]), sonstige Dateien als Link ([...]) referenzieren
          if (contentType.indexOf("image/") === 0) {
            attachmentLinks.push("![" + attName + "](" + encodedFolder + "/" + encodedFile + ")");
          } else {
            attachmentLinks.push("[" + attName + "](" + encodedFolder + "/" + encodedFile + ")");
          }
        }
      }

      // Falls Anhänge vorhanden sind, Links am Ende des Markdown-Dokuments anfügen
      if (attachmentLinks.length > 0) {
        markdownContent += "\n\n## Anhänge\n\n" + attachmentLinks.join("\n");
      }

      // Als Markdown-Datei speichern
      targetFolder.createFile(fileName, markdownContent, "text/markdown");
      threadExported = true;
    }

    // Thread als exportiert markieren, sobald mindestens eine Nachricht exportiert wurde
    if (threadExported) {
      threads[i].addLabel(label);
    }
  }
}

/**
 * Holt oder erstellt einen Unterordner innerhalb eines übergeordneten Google-Drive-Ordners.
 */
function getOrCreateSubfolder(parentFolder, subfolderName, cache) {
  if (cache && cache[subfolderName]) {
    return cache[subfolderName];
  }
  var folders = parentFolder.getFoldersByName(subfolderName);
  var folder = folders.hasNext() ? folders.next() : parentFolder.createFolder(subfolderName);
  if (cache) {
    cache[subfolderName] = folder;
  }
  return folder;
}

/**
 * Hilfsfunktion für das deutsche Datumsformat: "Sonntag 25. Oktober 2026 10:22:48"
 */
function formatGermanLongDate(date, timeZone) {
  var days = ["Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"];
  var months = [
    "Januar", "Februar", "März", "April", "Mai", "Juni",
    "Juli", "August", "September", "Oktober", "November", "Dezember"
  ];
  
  // Wochentag (1 = Montag ... 7 = Sonntag in ISO, daher über Utilities oder Date)
  var dayOfWeekIndex = parseInt(Utilities.formatDate(date, timeZone, "u"), 10) % 7;
  var dayName = days[dayOfWeekIndex];
  
  var dayOfMonth = Utilities.formatDate(date, timeZone, "dd");
  var monthIndex = parseInt(Utilities.formatDate(date, timeZone, "M"), 10) - 1;
  var monthName = months[monthIndex];
  var year = Utilities.formatDate(date, timeZone, "yyyy");
  var time = Utilities.formatDate(date, timeZone, "HH:mm:ss");
  
  return dayName + " " + dayOfMonth + ". " + monthName + " " + year + " " + time;
}
