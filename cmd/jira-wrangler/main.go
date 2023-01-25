package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	jira "github.com/andygrunwald/go-jira/v2/onpremise"
	"github.com/spf13/cobra"
	"github.com/thetechnick/jira-wrangler/internal/cli"
	jirainternal "github.com/thetechnick/jira-wrangler/internal/jira"
	"golang.org/x/exp/slices"
)

func main() {
	opts := Options{
		ConfigPath: "config.yaml",
	}

	cmd := &cobra.Command{
		Use:  "jira-wrangler",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(cmd.Root().Context())
			defer cancel()

			tp := jira.BearerAuthTransport{
				Token: opts.JiraToken,
			}
			client, err := jirainternal.NewClient(
				tp.Client(),
				jirainternal.WithBaseURL(opts.JiraURL),
			)
			if err != nil {
				return fmt.Errorf("setting up JIRA client: %w", err)
			}

			cfg, err := cli.LoadConfig(opts.ConfigPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			groups := make([]cli.Group, 0, len(cfg.Reports))

			for _, reportCfg := range cfg.Reports {
				issues, err := getIssuesGroupedByColor(ctx, reportCfg.Label, client)
				if err != nil {
					return fmt.Errorf("generating report: %w", err)
				}

				groups = append(groups, cli.Group{
					Title:  reportCfg.Title,
					Issues: issues,
				})
			}

			rw, err := cli.NewTemplatedReportWriter(
				os.Stdout,
				cli.WithOverrideTemplatePath(opts.OverrideTemplatesPath),
			)
			if err != nil {
				return fmt.Errorf("initializing report writer: %w", err)
			}

			if err := rw.WriteReport(cli.NewReport(cfg.Title, groups...)); err != nil {
				return fmt.Errorf("writing report header: %w", err)
			}

			return nil
		},
	}

	opts.AddFlags(cmd.Flags())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

type JiraClient interface {
	SearchIssues(ctx context.Context, jql string) ([]jirainternal.Issue, error)
	GetIssue(ctx context.Context, key string) (jirainternal.Issue, error)
}

func getIssuesGroupedByColor(ctx context.Context, label string, client JiraClient) ([]jirainternal.Issue, error) {
	jql := fmt.Sprintf(
		`project = "SDE" AND labels = %q AND Status in ("To Do","In Progress") ORDER BY priority DESC`,
		label,
	)
	issues, err := client.SearchIssues(ctx, jql)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(issues, func(a, b jirainternal.Issue) bool {
		return a.Color.Less(b.Color)
	})

	for idx, issue := range issues {
		fullIssue, err := client.GetIssue(ctx, issue.Key)
		if err != nil {
			return nil, err
		}

		issues[idx] = fullIssue
	}

	return issues, nil
}
