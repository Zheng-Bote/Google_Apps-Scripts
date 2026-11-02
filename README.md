# Gmail-to-Markdown Exporter & Google Drive Sync CLI

This project consists of two coordinated components for automatically transferring emails from Gmail as Markdown notes (including YAML front matter and attachments) via Google Drive into a local directory (for example, an Obsidian vault):

1. **[`exportGmailToMarkdown.js`](./exportGmailToMarkdown.js)** – A **Google Apps Script** that scans starred emails in Gmail, converts them to Markdown with YAML front matter, and stores them together with attachments in Google Drive.
2. **[`gdrive-sync` (`main.go`)](./main.go)** – A **Go (Golang v1.27)** CLI program that downloads the exported Markdown files and their attachment subfolders from Google Drive into a local folder and then cleans up the files in Google Drive after a successful download.

---

<!-- START doctoc -->
<!-- END doctoc -->

---

## Table of Contents

1. [Part 1: Google Apps Script (`exportGmailToMarkdown.js`)](#1-google-apps-script-exportgmailtomarkdownjs)
2. [Part 2: Go CLI Program (`gdrive-sync`)](#2-go-cli-program-gdrive-sync)
3. [Part 3: Create Google OAuth2 Client Credentials & Service Account](#3-create-google-oauth2-client-credentials--service-account)

---

## 1. Google Apps Script (`exportGmailToMarkdown.js`)

The script searches the Gmail mailbox for unprocessed emails whose subject ends with a category marker and saves them as `.md` files in Google Drive.

### Subject Syntax

| Marker at end of subject | Target folder in Google Drive     | Description                                          |
| :----------------------- | :-------------------------------- | :--------------------------------------------------- |
| `@snippet`               | `Gmail_Snippets/Snippets`         | Code snippets & short notes                          |
| `@tech`                  | `Gmail_Snippets/Tech`             | Technical articles, documentation & know-how         |
| `@priv`                  | `Gmail_Snippets/Private`          | Private notes & documents                            |
| `#<tag>`                 | YAML front matter `tags: [<tag>]` | Custom tags (multiple possible, e.g. `#golang #cli`) |

#### Examples of email subject lines:

- `Docker Container cleanup #docker #linux @snippet`
  - **Title / filename:** `Docker Container cleanup.md`
  - **Target folder:** `Gmail_Snippets/Snippets`
  - **Front matter:** `tags: [docker, linux]`
- `Kubernetes network architecture #k8s @tech`
  - **Title / filename:** `Kubernetes network architecture.md`
  - **Target folder:** `Gmail_Snippets/Tech`
  - **Front matter:** `tags: [k8s]`
- `Tax documents checklist @priv`
  - **Title / filename:** `Tax documents checklist.md`
  - **Target folder:** `Gmail_Snippets/Private`
  - **Front matter:** `tags: []`

### Handling Email Attachments

If an email contains file attachments (images, PDFs, archives, etc.):

1. In the relevant category folder (`Snippets`, `Tech`, or `Private`), a subfolder named after the Markdown file is created automatically: `<markdown-filename>/`.
2. All attachments are saved in that subfolder.
3. At the end of the generated Markdown file, an automatic section `## Attachments` is appended:
   - **Images (`image/*`)** are embedded as Markdown images: `![image.png](<Folder>/<image.png>)`
   - **Other files** are referenced as relative links: `[document.pdf](<Folder>/<document.pdf>)`

### Generated Markdown Format

```markdown
---
type: Note
title: Docker Container cleanup
description: "Short summary from the first line of the email"
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
modification date: Monday 02 November 2026 09:30:15
---

This is the actual body text of the email...

## Attachments

![screenshot.png](Docker%20Container%20cleanup/screenshot.png)
[guide.pdf](Docker%20Container%20cleanup/guide.pdf)
```

### Setup in Google Apps Script

1. Open [script.google.com](https://script.google.com/) and create a **new project**.
2. Copy the full content of [`exportGmailToMarkdown.js`](./exportGmailToMarkdown.js) into the editor (`Code.gs`).
3. Click **Run** at the top (`exportGmailToMarkdown`) the first time to grant permission for **Gmail** and **Google Drive**.
4. _(Optional)_ Configure a timed trigger under **Triggers (clock icon)** → **Add Trigger** to run automatically on a schedule (e.g. _Time-driven_ → _Every minute_ or _Every 15 minutes_), so new emails are exported automatically.
5. Processed emails are automatically labeled `Exported` in Gmail so they are not exported twice.

---

## 2. Go CLI Program (`gdrive-sync`)

The Go program queries a specified Google Drive folder, downloads all Markdown files contained there together with their corresponding attachment subfolders (`<markdown-filename>/`) into a local directory, and then deletes the files and attachments from Google Drive.

### Safety Mechanisms

- **Atomic download:** Files are first written to a temporary `.part` file, flushed to disk with `fsync`, and only renamed after the entire download is complete.
- **Delete only after success:** A Markdown file in Google Drive is only deleted if both the `.md` file itself and **all** files in the corresponding attachment subfolder `<markdown-filename>/` were downloaded successfully.
- **Preserve folder structure:** The base and category folders (`Gmail_Snippets`, `Snippets`, `Tech`, `Private`) are protected and remain in Google Drive for future exports; only the downloaded `.md` files and their attachment subfolders are deleted.

### Build

You need **Go 1.27** or newer:

```bash
go mod tidy
go build -o gdrive-sync .
```

### Command-Line Parameters

| Parameter       | Short form | Default            | Description                                                                                  |
| :-------------- | :--------- | :----------------- | :------------------------------------------------------------------------------------------- |
| `-drive-folder` | `-d`       | _(required)_       | Path of the Google Drive folder (e.g. `"Gmail_Snippets/Snippets"`) or direct Drive folder ID |
| `-local-dir`    | `-o`       | _(required)_       | Local target folder for saving downloaded files and attachments                              |
| `-credentials`  | –          | `credentials.json` | Path to the Google OAuth2 client credentials or service-account JSON file                    |
| `-token`        | –          | `token.json`       | Path to the locally cached OAuth2 token file                                                 |
| `-trash`        | –          | `false`            | Moves files in Google Drive to the trash instead of deleting them permanently                |
| `-dry-run`      | –          | `false`            | Simulation mode: shows what would be downloaded and deleted without actually doing it        |

### Usage Examples

#### Example 1: Download a single category folder (`Tech`) and save it locally

Downloads all Markdown files and attachments from `Gmail_Snippets/Tech` into the local folder `./notes/tech` and then deletes them from Google Drive:

```bash
./gdrive-sync \
  -drive-folder "Gmail_Snippets/Tech" \
  -local-dir "./notes/tech"
```

**Example output:**

```text
2026/11/02 09:55:01 Starting synchronization from Google Drive folder "Tech" (ID: 1a2b3c...) -> local "./notes/tech"
2026/11/02 09:55:02 Downloaded: "Kubernetes network architecture.md" (1420 bytes) -> notes/tech/Kubernetes network architecture.md
2026/11/02 09:55:03 Downloaded: "architecture.png" (84312 bytes) -> notes/tech/Kubernetes network architecture/architecture.png
2026/11/02 09:55:03 Deleted in Google Drive: "architecture.png" (ID: 9z8y7x...)
2026/11/02 09:55:04 Deleted in Google Drive: "Kubernetes network architecture" (ID: 5m4n3b...)
2026/11/02 09:55:04 Deleted in Google Drive: "Kubernetes network architecture.md" (ID: 2k3j4h...)
2026/11/02 09:55:04 Finished! All files and attachments were processed successfully.
```

#### Example 2: Synchronize the entire `Gmail_Snippets` folder (including `Snippets`, `Tech`, and `Private`)

```bash
./gdrive-sync -d "Gmail_Snippets" -o "/path/to/obsidian-vault/Inbox"
```

#### Example 3: Dry run (`-dry-run`) and trash mode (`-trash`)

```bash
# Check which files would be affected (without downloading/deleting):
./gdrive-sync -d "Gmail_Snippets/Snippets" -o "./snippets" -dry-run

# Move files to the Google Drive trash instead of deleting them permanently after download:
./gdrive-sync -d "Gmail_Snippets/Snippets" -o "./snippets" -trash
```

---

## 3. Create Google OAuth2 Client Credentials & Service Account

To allow `gdrive-sync` to access Google Drive, a `credentials.json` file is required from the [Google Cloud Console](https://console.cloud.google.com/). The program automatically detects whether it is using **OAuth2 desktop credentials** or a **service account**.

### Preparation (required for both variants)

1. Open the [Google Cloud Console](https://console.cloud.google.com/).
2. Create a **new project** from the project selector at the top (for example, `gdrive-markdown-sync`) or select an existing one.
3. In the menu, go to **APIs & Services** → **Library**.
4. Search for **Google Drive API**, select it, and click **Enable**.

---

### Variant A: OAuth 2.0 Client Credentials (Recommended for personal Google Drive)

This variant is best if the Google Apps Script runs under your personal Google account.

#### 1. Configure the OAuth consent screen

1. Go to **APIs & Services** → **OAuth consent screen** (or _Google Auth Platform_ → _Branding / Audience_).
2. Choose **External** as the user type (or _Internal_ for Google Workspace) and click **Create**.
3. Set an application name (for example, `GDrive Sync CLI`) and enter your email address as support and developer contact.
4. Under **Scopes**, add the scope `https://www.googleapis.com/auth/drive`.
5. Under **Test users**, add your own **Gmail address** (while the app is in the “Test” phase, only listed test users may sign in).

#### 2. Create an OAuth client ID and download `credentials.json`

1. Go to **APIs & Services** → **Credentials**.
2. Click **+ Create Credentials** → **OAuth client ID**.
3. Select **Desktop app** as the application type and give it a name (for example, `gdrive-sync-cli`).
4. Click **Create** and then **Download JSON** in the dialog.
5. Save the downloaded file as `credentials.json` in the program directory (next to `gdrive-sync`).

#### 3. One-time sign-in on first run

When you run `./gdrive-sync` for the first time, the program prints a link in the terminal:

1. Open the displayed link in the browser and sign in with your Google account.
2. Confirm access (in your own test project, you may need to use _Advanced_ → _Go to ... (unsafe)_).
3. The CLI program automatically receives the code via a temporarily opened local callback port (`127.0.0.1`) and stores the refresh token in `token.json`. All future runs will work automatically without browser interaction.

---

### Variant B: Service Account JSON File (For server / headless use without browser)

A service account has its own technical email address (`...@<project>.iam.gserviceaccount.com`) and does not require browser login.

#### 1. Create the service account and download the JSON key

1. In the Google Cloud Console, go to **APIs & Services** → **Credentials** (or **IAM & Admin** → **Service Accounts**).
2. Click **+ Create Credentials** → **Service account**.
3. Enter a name (for example, `gdrive-sync-bot`) and click **Create and continue** → **Done**.
4. Click the newly created service account in the list and switch to the **Keys** tab at the top.
5. Click **Add Key** → **Create new key**, select **JSON**, and click **Create**.
6. Save the downloaded JSON file as `credentials.json` in the program directory (or pass it with `-credentials /path/to/service-account.json`).

#### 2. Important: Share the Google Drive folder with the service account

Because a service account is its own account, it cannot see your personal Drive folders by default:

1. Copy the service account email address from the JSON file (field `"client_email"`).
2. Open [Google Drive](https://drive.google.com/), right-click the **`Gmail_Snippets`** folder → **Share**.
3. Add the service account email with the permission **Editor** (so it can download and later delete files).

## Links

[md_indexer-searcher](https://github.com/Zheng-Bote/md_indexer-searcher)
[convert_to_markdown](https://github.com/Zheng-Bote/convert_to_markdown)
