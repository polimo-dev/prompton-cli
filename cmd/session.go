package cmd

import (
	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/config"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

func newLogoutCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke this CLI session and forget it",
		Long: `Revoke the stored session token server-side, then clear it from the config
file. The host is kept, so a later ` + "`login`" + ` against a self-hosted instance
does not need --host again.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := g.config()
			if err != nil {
				return err
			}
			p := g.printer()

			if cfg.Token == "" {
				if g.asJSON {
					return p.PrintJSON(map[string]any{"revoked": false, "reason": "no stored session"})
				}
				p.Info("Not logged in.")
				return nil
			}

			revoked := true
			if err := api.New(cfg.Host, cfg.Token).RevokeSession(cmd.Context()); err != nil {
				// The local credential is cleared regardless: a token the
				// server already rejects is not worth keeping on disk.
				revoked = false
				if apiErr, ok := api.AsError(err); ok && apiErr.Status == 401 {
					p.Warn("The session was already invalid server-side.")
				} else {
					p.Warn("Could not revoke the session server-side: %v", err)
				}
			}

			if err := config.WriteFile(cfg.Path, config.Cleared(cfg.File)); err != nil {
				return err
			}
			if g.asJSON {
				return p.PrintJSON(map[string]any{"revoked": revoked, "config": cfg.Path})
			}
			p.Info("Logged out.")
			return nil
		},
	}
}

func newWhoamiCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in user and their organizations",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, cfg, err := g.client()
			if err != nil {
				return err
			}
			me, err := client.Me(cmd.Context())
			if err != nil {
				return err
			}
			p := g.printer()

			if g.asJSON {
				return p.PrintJSON(map[string]any{
					"host":          cfg.Host,
					"user":          me.User,
					"organizations": me.Orgs,
					"org":           cfg.Org,
					"project":       cfg.Project,
				})
			}

			p.Fields([][2]string{
				{"Host", cfg.Host},
				{"User", displayUser(me.User)},
				{"User id", me.User.ID},
				{"Org", output.Dash(cfg.Org)},
				{"Project", output.Dash(cfg.Project)},
			})
			if len(me.Orgs) > 0 {
				p.Info("")
				p.Info("Organizations:")
				rows := make([][]string, 0, len(me.Orgs))
				for _, o := range me.Orgs {
					rows = append(rows, orgRow(o))
				}
				p.Table([]string{"REF", "NAME", "KIND", "ID"}, rows)
			}
			return nil
		},
	}
}

func newOrgsCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orgs",
		Short: "Organizations you belong to",
		Args:  noArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List your organizations",
		Args:  noArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			orgs, err := client.ListOrgs(c.Context())
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"organizations": orgs})
			}
			rows := make([][]string, 0, len(orgs))
			for _, o := range orgs {
				rows = append(rows, orgRow(o))
			}
			p.Table([]string{"REF", "NAME", "KIND", "ID"}, rows)
			return nil
		},
	})
	return cmd
}

func newUseCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "use",
		Short: "Set the default organization and project",
		Long: `Remember an organization (and optionally a project) so later commands do not
need --org and --project.

Both are verified against the server before they are stored, so a typo fails
here rather than three commands later.`,
		Example: "  " + meta.Name + " use --org acme --project helpdesk\n  " +
			meta.Name + " use --org personal",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if g.org == "" && g.project == "" {
				return usagef("nothing to set — pass --org and/or --project")
			}
			client, cfg, err := g.client()
			if err != nil {
				return err
			}
			file := cfg.File
			p := g.printer()

			if g.org != "" {
				org, err := client.GetOrg(cmd.Context(), g.org)
				if err != nil {
					if api.Is(err, api.CodeNotFound) {
						return failf("no organization %q is visible to you", g.org)
					}
					return err
				}
				if org.Ref() != file.Org {
					// Projects belong to an org; keep no stale pointer.
					file.Project = ""
				}
				file.Org = org.Ref()
			}

			if g.project != "" {
				// The project is verified against the org in effect for this
				// run, which may have come from --org or the environment even
				// though only the flag is written back to the file.
				org := file.Org
				if org == "" {
					org = cfg.Org
				}
				if org == "" {
					return usagef("select an organization first: --org <slug|personal>")
				}
				projects, err := client.ListProjects(cmd.Context(), org)
				if err != nil {
					return err
				}
				found := false
				known := make([]string, 0, len(projects))
				for _, pr := range projects {
					known = append(known, pr.Slug)
					if pr.Slug == g.project {
						found = true
					}
				}
				if !found {
					return failf("no project %q in %s (have: %s)", g.project, org, output.Join(known))
				}
				file.Project = g.project
			}

			if err := config.WriteFile(cfg.Path, file); err != nil {
				return err
			}
			if g.asJSON {
				return p.PrintJSON(map[string]any{"org": file.Org, "project": file.Project, "config": cfg.Path})
			}
			p.Fields([][2]string{
				{"Org", output.Dash(file.Org)},
				{"Project", output.Dash(file.Project)},
			})
			return nil
		},
	}
}
