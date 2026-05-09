package cli

// PATCH: novel-commands — see .printing-press-patches.json for the change-set rationale.

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/auth"
)

// newAuthCmd manages session cookies imported from Chrome for OpenTable + Tock.
// There is no API key flow; both services use cookie-session auth and the user
// is expected to have logged in to opentable.com and exploretock.com in Chrome.
func newAuthCmd(flags *rootFlags) *cobra.Command {
	// No mcp:read-only on the parent — this is a command group whose
	// children include both reads (`status`) and writes (`login`,
	// `logout`). Per-subcommand annotations carry the right hint.
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage browser cookie sessions for OpenTable and Tock",
		Long: "Import session cookies from your local Chrome profile so authenticated " +
			"commands (book, my-reservations, wishlist) work without an API key.",
	}
	cmd.AddCommand(newAuthLoginCmd(flags))
	cmd.AddCommand(newAuthStatusCmd(flags))
	cmd.AddCommand(newAuthLogoutCmd(flags))
	return cmd
}

func newAuthLoginCmd(flags *rootFlags) *cobra.Command {
	var fromChrome bool
	// `auth login --chrome` writes session cookies to disk via
	// `session.Save()`. Do NOT mark mcp:read-only — an MCP host that
	// honors the hint would skip the call in side-effect-prohibited
	// contexts and the user-consented session import would silently
	// not happen.
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Import session cookies from your local Chrome profile",
		Long: "Reads cookies from your local Chrome cookie store, filters for opentable.com " +
			"and exploretock.com, and saves them to ~/.config/table-reservation-goat-pp-cli/session.json.\n\n" +
			"On macOS, Chrome encrypts cookies with a key in the system keychain — you may be " +
			"prompted by macOS to authorize keychain access.",
		Example: "  table-reservation-goat-pp-cli auth login --chrome",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliutil.IsVerifyEnv() {
				fmt.Fprintln(cmd.OutOrStdout(), "would import cookies from chrome (verify mode — skipping)")
				return nil
			}
			if !fromChrome {
				return fmt.Errorf("specify --chrome to import from your local Chrome profile (no other source supported in v1)")
			}
			otCookies, tockCookies, result, err := auth.ImportFromChrome(cmd.Context())
			if err != nil {
				return fmt.Errorf("importing chrome cookies: %w", err)
			}
			session, err := auth.Load()
			if err != nil {
				return fmt.Errorf("loading existing session: %w", err)
			}
			session.OpenTableCookies = otCookies
			session.TockCookies = tockCookies
			if err := session.Save(); err != nil {
				return fmt.Errorf("saving session: %w", err)
			}
			// `auth login --chrome` is the user's deliberate refresh path.
			// Clear any negative-cache entry so the next OT client re-walks
			// the keychain (which is now warm from this very kooky call) and
			// caches the fresh Akamai cookies.
			auth.ClearAkamaiCache("opentable.com")
			auth.ClearAkamaiCache("exploretock.com")
			out := map[string]any{
				"opentable_imported":  result.OpenTableImported,
				"tock_imported":       result.TockImported,
				"opentable_skipped":   result.OpenTableSkipped,
				"tock_skipped":        result.TockSkipped,
				"opentable_logged_in": session.LoggedIn(auth.NetworkOpenTable),
				"tock_logged_in":      session.LoggedIn(auth.NetworkTock),
			}
			if len(result.Notes) > 0 {
				out["notes"] = result.Notes
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().BoolVar(&fromChrome, "chrome", false, "Import session cookies from local Chrome profile (required in v1)")
	return cmd
}

func newAuthStatusCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether OpenTable and Tock cookies are loaded and fresh",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			session, err := auth.Load()
			if err != nil {
				return fmt.Errorf("loading session: %w", err)
			}
			path, _ := auth.SessionPath()
			out := map[string]any{
				"session_path":        path,
				"updated_at":          session.UpdatedAt,
				"opentable_logged_in": session.LoggedIn(auth.NetworkOpenTable),
				"tock_logged_in":      session.LoggedIn(auth.NetworkTock),
				"opentable_cookies":   len(session.OpenTableCookies),
				"tock_cookies":        len(session.TockCookies),
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
}

func newAuthLogoutCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the local session (does not log out from Chrome itself)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliutil.IsVerifyEnv() {
				fmt.Fprintln(cmd.OutOrStdout(), "would clear session (verify mode — skipping)")
				return nil
			}
			if err := auth.Clear(); err != nil {
				return err
			}
			return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"cleared": true}, flags)
		},
	}
}
