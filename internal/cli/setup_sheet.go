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
// Nothing is saved unless it has been tested first. If the sheet's owner
// still has to approve sharing it with the tool, the webmaster can save
// their progress (the key, which Google has accepted, and the sheet's ID)
// and finish once it's approved.
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
				"Nothing is saved until it has been tested."+pendingNote(cfg.Sheet)),
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

	// saveKey stores a new key (or its 1Password reference) in cfg/the store.
	saveKey := func() (string, error) {
		switch {
		case keyRef != "":
			cfg.Secrets.GoogleServiceAccountKeyRef = keyRef
			return "key: 1Password (" + keyRef + ")", nil
		case newKey:
			return saveGeneratedSecret(ctx, out, cfg, generatedSecret{
				key:      secrets.GoogleServiceAccountKey,
				value:    key.Compact(),
				itemName: "APS Google Sheet key (aps-tools)",
				warning:  "It's the key file's contents, squeezed onto one line.",
				rerun:    rerun,
				explain:  sheet.ExplainStoredKey,
			})
		}
		return "key: unchanged", nil
	}

	// 1. Prove the key itself works, before worrying about sharing.
	lipgloss.Fprintln(out, dimStyle.Render("Signing in to Google as "+key.ClientEmail+"…"))
	if err := client.SignIn(ctx); err != nil {
		return failed(out, err.Error(), rerun)
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Google accepted the key.")

	// 2. Make sure the sheet is shared with the robot, then read it.
	var book sheet.Spreadsheet
	for {
		lipgloss.Fprintln(out, dimStyle.Render("Reading the sheet…"))
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
		lipgloss.Fprintln(out, dimStyle.Render("  Can't share it yourself? Google sends the owner a request to approve."))
		lipgloss.Fprintln(out)

		next := "retry"
		ask := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().
			Title("What now?").
			Options(
				huh.NewOption("It's shared (or the owner approved): try again", "retry"),
				huh.NewOption("I've requested access: save my progress and finish once it's approved", "later"),
				huh.NewOption("Stop without saving anything", "stop"),
			).
			Value(&next))).WithTheme(formTheme)
		if err := ask.RunWithContext(ctx); err != nil {
			return cancelled(err)
		}
		switch next {
		case "stop":
			lipgloss.Fprintln(out, dimStyle.Render("Nothing was saved. Run again when it's shared: "+rerun))
			return errSilent
		case "later":
			// Save what's been proven to work (the key, the sheet's ID) and
			// remember that a share is waiting on the owner.
			where, err := saveKey()
			if err != nil {
				return err
			}
			cfg.Sheet = config.Sheet{
				SpreadsheetID:   id,
				Tab:             keepIfSameSheet(cfg.Sheet, id),
				ServiceAccount:  key.ClientEmail,
				AccessRequested: true,
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Progress saved"+dimStyle.Render("  ·  "+where))
			offerToDeleteDownload(ctx, out, newKey && keyRef == "", keyPath)
			lipgloss.Fprintln(out)
			lipgloss.Fprintln(out, "Once the owner approves, finish with:  "+boldStyle.Render(rerun))
			lipgloss.Fprintln(out, dimStyle.Render("The link and key are remembered: press Enter twice, then pick the tab."))
			return nil
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
	where, err := saveKey()
	if err != nil {
		return err
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
	offerToDeleteDownload(ctx, out, newKey && keyRef == "", keyPath)
	lipgloss.Fprintln(out)
	lipgloss.Fprintln(out, "Next, see what the tool found in the sheet:  "+boldStyle.Render("aps-tools sheet discover"))
	return nil
}

// keepIfSameSheet keeps the saved tab when setup is re-run for the same
// spreadsheet, so a pending share doesn't forget the earlier choice.
func keepIfSameSheet(saved config.Sheet, id string) string {
	if saved.SpreadsheetID == id {
		return saved.Tab
	}
	return ""
}

// offerToDeleteDownload asks to delete the downloaded key file once the key
// is safely stored: the copy in Downloads would otherwise just lie around.
func offerToDeleteDownload(ctx context.Context, out io.Writer, ask bool, keyPath string) {
	if !ask {
		return
	}
	file := sheet.CleanPath(keyPath)
	del := true
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("Delete the downloaded key file?").
		Description(file + "\nThe key is saved now, so this copy isn't needed. Recommended.").
		Affirmative("Delete it").
		Negative("Keep it").
		Value(&del))).WithTheme(formTheme)
	if err := form.RunWithContext(ctx); err != nil || !del {
		return
	}
	if err := os.Remove(file); err != nil {
		lipgloss.Fprintln(out, warnStyle.Render("! ")+"Couldn't delete it ("+err.Error()+"). Delete it yourself.")
		return
	}
	lipgloss.Fprintln(out, okStyle.Render("✓ ")+"Deleted the downloaded copy.")
}

// pendingNote reminds the webmaster when a share request was left waiting.
func pendingNote(s config.Sheet) string {
	if !s.AccessRequested {
		return ""
	}
	return "\n\nLast time you requested access to the sheet. If the owner has\napproved it, this run finishes setup."
}
