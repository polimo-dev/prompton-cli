package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/polimo-dev/prompton-cli/internal/api"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"github.com/polimo-dev/prompton-cli/internal/output"
)

func newProjectsCommand(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Projects in an organization",
		Args:  noArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newProjectsListCommand(g), newProjectsCreateCommand(g))
	return cmd
}

func newProjectsListCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the organization's projects",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, err := g.orgRef()
			if err != nil {
				return err
			}
			projects, err := client.ListProjects(cmd.Context(), org)
			if err != nil {
				return err
			}
			p := g.printer()
			if g.asJSON {
				return p.PrintJSON(map[string]any{"projects": projects})
			}
			rows := make([][]string, 0, len(projects))
			for _, pr := range projects {
				rows = append(rows, []string{pr.Slug, pr.Name, pr.Timezone, environmentList(pr.Environments), output.Date(pr.CreatedAt)})
			}
			p.Table([]string{"SLUG", "NAME", "TIMEZONE", "ENVIRONMENTS", "CREATED"}, rows)
			return nil
		},
	}
}

func newProjectsCreateCommand(g *globals) *cobra.Command {
	var name, timezone string

	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a project",
		Long: `Create a project. Its production (protected) and staging environments are
created with it.

The slug is lowercase letters, digits and hyphens, unique inside the
organization.`,
		Example: "  " + meta.Name + " projects create helpdesk --name Helpdesk",
		Args:    exactArgs(1, "<slug>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := g.client()
			if err != nil {
				return err
			}
			org, err := g.orgRef()
			if err != nil {
				return err
			}
			slug := args[0]

			project, createErr := client.CreateProject(cmd.Context(), org, api.CreateProjectRequest{
				Key:      slug,
				Name:     name,
				Timezone: timezone,
			})
			var found api.Project
			wasExisting, err := existing(createErr, &found)
			if err != nil {
				return err
			}
			if wasExisting {
				project = &found
			}

			p := g.printer()
			if g.asJSON {
				if err := p.PrintJSON(project); err != nil {
					return err
				}
			} else {
				p.Fields([][2]string{
					{"Slug", project.Slug},
					{"Name", project.Name},
					{"Timezone", project.Timezone},
					{"Environments", environmentList(project.Environments)},
					{"Id", project.ID},
				})
			}
			if wasExisting {
				return g.alreadyExists("project", slug)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "display name (defaults to the slug)")
	cmd.Flags().StringVar(&timezone, "timezone", "", "IANA timezone for reporting (default Etc/UTC)")
	return cmd
}

func environmentList(envs []api.Environment) string {
	if len(envs) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(envs))
	for _, e := range envs {
		if e.Protected {
			parts = append(parts, e.Slug+"*")
			continue
		}
		parts = append(parts, e.Slug)
	}
	return strings.Join(parts, ",")
}
