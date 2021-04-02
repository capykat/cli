package execute

import (
	"context"
	"flag"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/airplanedev/cli/pkg/api"
	"github.com/airplanedev/cli/pkg/cli"
	"github.com/airplanedev/cli/pkg/logger"
	"github.com/airplanedev/cli/pkg/print"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	bold = color.New(color.Bold).SprintfFunc()
	gray = color.New(color.FgHiBlack).SprintfFunc()
)

// Config are the execute config.
type config struct {
	root *cli.Config
	slug string
	args []string
}

// New returns a new execute cobra command.
func New(c *cli.Config) *cobra.Command {
	var cfg = config{root: c}

	cmd := &cobra.Command{
		Use:   "execute <slug>",
		Short: "Execute a task",
		Long:  "Execute a task by its slug with the provided parameters.",
		Example: heredoc.Doc(`
			airplane tasks execute echo -- --name value
			airplane tasks execute <slug> -- [parameters]
		`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.slug = args[0]
			cfg.args = args[1:]
			return run(cmd.Context(), cfg)
		},
	}

	return cmd
}

// Run runs the execute command.
func run(ctx context.Context, cfg config) error {
	var client = cfg.root.Client

	task, err := client.GetTask(ctx, cfg.slug)
	if err != nil {
		return errors.Wrap(err, "get task")
	}

	req := api.RunTaskRequest{
		TaskID:     task.ID,
		Parameters: make(api.Values),
	}
	set := flagset(task, req.Parameters)

	if err := set.Parse(cfg.args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	logger.Log(gray("Running: %s", task.Name))

	w, err := client.Watcher(ctx, req)
	if err != nil {
		return err
	}

	logger.Log(gray("Queued: %s", client.RunURL(w.RunID())))

	var state api.RunState
	agentPrefix := "[agent]"
	outputPrefix := "airplane_output"

	for {
		if state = w.Next(); state.Err() != nil {
			break
		}

		for _, l := range state.Logs {
			var loggedText string
			if strings.HasPrefix(l.Text, agentPrefix) {
				// De-emphasize agent logs and remove prefix
				loggedText = gray(strings.TrimLeft(strings.TrimPrefix(l.Text, agentPrefix), " "))
			} else if strings.HasPrefix(l.Text, outputPrefix) {
				// De-emphasize outputs appearing in logs
				loggedText = gray(l.Text)
			} else {
				// Try to leave user logs alone, so they can apply their own colors
				loggedText = l.Text
			}
			logger.Log(loggedText)
		}

		if state.Stopped() {
			break
		}
	}

	if err := state.Err(); err != nil {
		return err
	}

	print.Outputs(state.Outputs)

	status := string(state.Status)
	switch state.Status {
	case api.RunSucceeded:
		status = color.GreenString(status)
	case api.RunFailed, api.RunCancelled:
		status = color.RedString(status)
	}
	logger.Log(bold(status))

	if state.Failed() {
		return errors.New("Run has failed")
	}

	return nil
}

// Flagset returns a new flagset from the given task parameters.
func flagset(task api.Task, args api.Values) *flag.FlagSet {
	var set = flag.NewFlagSet(task.Name, flag.ContinueOnError)

	set.Usage = func() {
		logger.Log("\n%s Usage:", task.Name)
		set.VisitAll(func(f *flag.Flag) {
			logger.Log("  --%s %s (default: %q)", f.Name, f.Usage, f.DefValue)
		})
		logger.Log("")
	}

	for _, p := range task.Parameters {
		var slug = p.Slug
		var typ = p.Type
		var def = p.Default

		set.Func(p.Slug, p.Desc, func(v string) (err error) {
			if v == "" {
				args[slug] = def
				return nil
			}

			switch typ {
			case api.TypeString:
				args[slug] = v

			case api.TypeBoolean:
				b, err := strconv.ParseBool(v)
				if err != nil {
					return err
				}
				args[slug] = b

			case api.TypeInteger:
				n, err := strconv.Atoi(v)
				if err != nil {
					return err
				}
				args[slug] = n

			case api.TypeFloat:
				n, err := strconv.ParseFloat(v, 64)
				if err != nil {
					return err
				}
				args[slug] = n

			case api.TypeUpload:
				// TODO(amir): we need to support them with some special
				// character perhaps `@` like curl?
				return errors.New("uploads are not supported from the CLI")

			case api.TypeDate:
				args[slug] = v

			case api.TypeDatetime:
				args[slug] = v
			}

			return nil
		})
	}

	return set
}
