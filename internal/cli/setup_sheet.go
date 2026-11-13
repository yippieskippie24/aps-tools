package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/yippieskippie24/aps-tools/internal/clipboard"
	"github.com/yippieskippie24/aps-tools/internal/config"
	"github.com/yippieskippie24/aps-tools/internal/connectors/sheet"
	"github.com/yippieskippie24/aps-tools/internal/secrets"
)

// setupSheet is the guided setup for the membership Google Sheet
// (`aps-tools setup sheet`). Guide: docs/setup/google-sheet.md.
// Nothing is saved unless the sheet can be read first.
func setupSheet(ctx context.Context, out io.Writer) error {
	if err := requireTerminal(); err != nil {
		return err
	}
	cfg, err := config.LoadOrEmpty()
	if err != nil {
		return err
	}
	const rerun = "aps-tools setup sheet"

	// Is there already a working key? Then the file question is optional.
	var saved sheet.ServiceAccountKey
	haveSaved := false
	if raw, err := secrets.ForConfig(cfg).Get(secrets.GoogleServiceAccountKey); err == nil {
		if k, err := sheet.ParseServiceAccountKey(raw); err == nil {
			saved, haveSaved = k, true
		}
	}

	link := cfg.Sheet.SpreadsheetID
	keyPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		if found := sheet.FindKeyFiles(filepath.Join(home, "Downloads")); len(found) > 0 {
			keyPath = found[0]
		}
	}
	keyDesc := "The .json file Google Cloud downloaded (see the guide). Tip: drag the file\ninto this window to type its location."
	if cfg.Secrets.UsesOnePassword() {
		keyDesc += "\nAlready attached the file to a 1Password item? Give its secret reference\ninstead, e.g. op://Private/APS Google Sheet key/aps-tools-key.json"
	}
	if haveSaved {
		keyDesc = "A key is already saved (" + saved.ClientEmail + ").\nLeave this blank to keep it, or give a new key file to replace it."
		keyPath = ""
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Connect the membership Google Sheet").
			Description("The tool reads the sheet as a Google \"service account\", a robot\n"+
				"account the sheet is shared with. Step-by-step guide:\ndocs/setup/google-sheet.md\n\n"+
				"Nothing is saved until the sheet has been read successfully."),
		huh.NewInput().
			Title("Link to the membership sheet").
			Description("Open the sheet in your browser and copy the address bar.").
			Placeholder("https://docs.google.com/spreadsheets/d/…/edit").
			Value(&link).
			Validate(func(s string) error {
				if len(sheet.SpreadsheetID(s)) < 20 {
					return errors.New("paste the sheet's full link from your browser's address bar")
				}
				return nil
			}),
		huh.NewInput().
			Title("Service account key file").
			Description(keyDesc).
			Placeholder("~/Downloads/aps-tools-….json").
			Value(&keyPath).
			Validate(func(p string) error {
				if strings.TrimSpace(p) == "" {
					if haveSaved {
						return nil
					}
					return errors.New("give the location of the downloaded .json key file")
				}
				if cfg.Secrets.UsesOnePassword() && secrets.LooksLikeReference(p) {
					return nil // read after the form, so 1Password asks only once
				}
				_, err := sheet.ReadKeyFile(p)
				return err
			}),
	)).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil {
		return cancelled(err)
	}

	// The key comes from a downloaded file, or (1Password) from a file
	// already attached to a 1Password item, given by its secret reference.
	key, newKey, keyRef := saved, false, ""
	switch {
	case strings.TrimSpace(keyPath) == "":
		// keeping the saved key
	case cfg.Secrets.UsesOnePassword() && secrets.LooksLikeReference(keyPath):
		keyRef = secrets.CleanReference(keyPath)
		lipgloss.Fprintln(out, dimStyle.Render("Reading the key from 1Password… (approve the prompt if one appears)"))
		raw, err := secrets.ReadReference(keyRef)
		if err != nil {
			return failed(out, err.Error(), rerun)
		}
		if key, err = sheet.ParseServiceAccountKey(raw); err != nil {
			return failed(out, "That 1Password reference doesn't hold the key file: "+err.Error(), rerun)
		}
		newKey = true
	default:
		if key, err = sheet.ReadKeyFile(keyPath); err != nil {
			return failed(out, err.Error(), rerun)
		}
		newKey = true
	}
	id := sheet.SpreadsheetID(link)
	client := sheet.NewClient(key)

	// 1. Make sure the sheet is shared with the robot, then read it.
	var book sheet.Spreadsheet
	for {
		lipgloss.Fprintln(out, dimStyle.Render("Reading the sheet as "+key.ClientEmail+"…"))
		book, err = client.GetSpreadsheet(ctx, id)
		if err == nil {
			break
		}
		lipgloss.Fprintln(out, failStyle.Render("✗ ")+err.Error())
		_ = clipboard.Copy(key.ClientEmail)
		lipgloss.Fprintln(out)
		lipgloss.Fprintln(out, boldStyle.Render("Share the sheet with the tool")+dimStyle.Render("  (its address is on your clipboard)"))
		lipgloss.Fprintln(out, "  1. Open the sheet → "+boldStyle.Render("Share")+".")
		lipgloss.Fprintln(out, "  2. Paste "+boldStyle.Render(key.ClientEmail)+", set it to "+boldStyle.Render("Editor")+",")
		lipgloss.Fprintln(out, "     untick \"Notify people\" (it's a robot), and click Share.")
		lipgloss.Fprintln(out, dimStyle.Render("  Editor is for writing back setup status in a later version. For now the tool only reads."))
		lipgloss.Fprintln(out, dimStyle.Render("  Can't share it yourself? Google sends the owner a request. Stop here and run setup again once it's approved."))
		again := true
		retry := huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Try reading the sheet again?").
			Affirmative("Yes, I've shared it").
			Negative("Stop").
			Value(&again))).WithTheme(formTheme)
		if err := retry.RunWithContext(ctx); err != nil || !again {
			lipgloss.Fprintln(out, dimStyle.Render("Nothing was saved. Run again when it's shared: "+rerun))
			return errSilent
		}
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Can read "+boldStyle.Render(book.Title)+".")
	if len(book.Tabs) == 0 {
		return failed(out, "That spreadsheet has no tabs with cells in it.", rerun)
	}

	// 2. Which tab lists the members?
	tab := sheet.SuggestTab(book.Tabs, cfg.Sheet.Tab)
	if len(book.Tabs) > 1 {
		var opts []huh.Option[string]
		for _, t := range book.Tabs {
			opts = append(opts, huh.NewOption(t, t))
		}
		pick := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().
			Title("Which tab lists the members?").
			Description("The sheet has several tabs. Pick the current member list.").
			Options(opts...).
			Value(&tab))).WithTheme(formTheme)
		if err := pick.RunWithContext(ctx); err != nil {
			return cancelled(err)
		}
	}
	rows, err := client.Rows(ctx, id, tab)
	if err != nil {
		return failed(out, err.Error(), rerun)
	}
	cols, ok := sheet.FindColumns(rows)
	if !ok {
		return failed(out, "Couldn't find an email column on the \""+tab+"\" tab. The tool looks for a header containing \"mail\" in the first 10 rows.", rerun)
	}
	members, _ := sheet.Members(rows, cols)
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+fmt.Sprintf("Found %d members on the %q tab (headers on row %d).", len(members), tab, cols.HeaderRow+1))
	for _, r := range sheet.Roles {
		if j, ok := cols.Index[r]; ok {
			lipgloss.Fprintln(out, dimStyle.Render(fmt.Sprintf("    %-20s column %s  %q", r, sheet.ColumnLetter(j), cols.Headers[r])))
		} else {
			lipgloss.Fprintln(out, warnStyle.Render("    ! ")+dimStyle.Render(string(r)+": not found"))
		}
	}
	if !cols.Found(sheet.RoleVetted) {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"No \"App vetted\" column found, so nobody will show as ready to set up.")
	}

	// 3. Save — everything has been tested.
	where := "key: unchanged"
	if keyRef != "" {
		cfg.Secrets.GoogleServiceAccountKeyRef = keyRef
		where = "key: 1Password (" + keyRef + ")"
	} else if newKey {
		if where, err = saveGeneratedSecret(ctx, out, cfg, generatedSecret{
			key:      secrets.GoogleServiceAccountKey,
			value:    key.Compact(),
			itemName: "APS Google Sheet key (aps-tools)",
			warning:  "It's the key file's contents, squeezed onto one line.",
			rerun:    rerun,
			explain:  sheet.ExplainStoredKey,
		}); err != nil {
			return err
		}
	}
	cfg.Sheet = config.Sheet{
		SpreadsheetID:    id,
		SpreadsheetTitle: book.Title,
		Tab:              tab,
		ServiceAccount:   key.ClientEmail,
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	path, _ := config.Path()
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Saved. Settings: "+path+dimStyle.Render("  ·  "+where))

	// 4. The downloaded file is a copy of the key lying around in Downloads.
	if newKey && keyRef == "" {
		file := sheet.CleanPath(keyPath)
		del := true
		ask := huh.NewForm(huh.NewGroup(huh.NewConfirm().
			Title("Delete the downloaded key file?").
			Description(file + "\nThe key is saved now, so this copy isn't needed. Recommended.").
			Affirmative("Delete it").
			Negative("Keep it").
			Value(&del))).WithTheme(formTheme)
		if err := ask.RunWithContext(ctx); err == nil && del {
			if err := os.Remove(file); err != nil {
				lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't delete it ("+err.Error()+"). Delete it yourself.")
			} else {
				lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Deleted the downloaded copy.")
			}
		}
	}
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Next, see what the tool found in the sheet:  "+boldStyle.Render("aps-tools sheet discover"))
	return nil
}
