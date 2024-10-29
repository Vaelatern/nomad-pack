// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"github.com/posener/complete"

	"github.com/hashicorp/nomad-pack/internal/pkg/cache"
	"github.com/hashicorp/nomad-pack/internal/pkg/errors"
	"github.com/hashicorp/nomad-pack/internal/pkg/flag"
	"github.com/hashicorp/nomad-pack/terminal"
)

// RenderFSCommand is a command that allows users to render the templates within
// a pack and see them as a filesystem. This is primarily so you can have a source of
// truth other than your nomad server.
type RenderFSCommand struct {
	*baseCommand
	packConfig *cache.PackConfig

	// renderOutputTemplate is a boolean flag to control whether the output
	// template is rendered.
	renderOutputTemplate bool

	// noRenderAuxFiles is a boolean flag to control whether we should also render
	// auxiliary files inside templates/
	noRenderAuxFiles bool

	// noFormat is a boolean flag to control whether we should hcl-format the
	// templates before rendering them.
	noFormat bool

	// overwriteAll is set to true when someone specifies "a" to the y/n/a
	overwriteAll bool
}

type RenderFS struct {
	Name    string
	Content string
}

func (r RenderFS) toTerminal(c *RenderFSCommand) {
	c.ui.Output(r.Name+":", terminal.WithStyle(terminal.BoldStyle))
	c.ui.Output("")
	c.ui.Output(r.Content)
}

func (r RenderFS) toFile(c *RenderFSCommand, ec *errors.UIErrorContext) error {
	return nil
}

// Run satisfies the Run function of the cli.Command interface.
func (c *RenderFSCommand) Run(args []string) int {
	c.cmdKey = "render fs" // Add cmdKey here to print out helpUsageMessage on Init error

	if err := c.Init(
		WithExactArgs(1, args),
		WithFlags(c.Flags()),
		WithNoConfig(),
	); err != nil {
		c.ui.ErrorWithContext(err, ErrParsingArgsOrFlags)
		c.ui.Info(c.helpUsageMessage())
		return 1
	}

	c.packConfig.Name = c.args[0]

	// Set the packConfig defaults if necessary and generate our UI error context.
	errorContext := initPackCommand(c.packConfig)

	if err := cache.VerifyPackExists(c.packConfig, errorContext, c.ui); err != nil {
		return 1
	}

	client, err := c.getAPIClient()
	if err != nil {
		c.ui.ErrorWithContext(err, "failed to initialize client", errorContext.GetAll()...)
		return 1
	}
	packManager := generatePackManager(c.baseCommand, client, c.packConfig)

	renderOutput, err := renderPack(
		packManager,
		c.baseCommand.ui,
		!c.noRenderAuxFiles,
		!c.noFormat,
		c.baseCommand.ignoreMissingVars,
		errorContext,
	)
	if err != nil {
		return 1
	}

	// The render command should at least render one parent, or one dependant
	// pack template.
	if renderOutput.LenParentRenders() < 1 && renderOutput.LenDependentRenders() < 1 {
		c.ui.ErrorWithContext(errors.ErrNoTemplatesRendered, "no templates rendered", errorContext.GetAll()...)
		return 1
	}

	var renders []RenderFS

	// Iterate the rendered files and add these to the list of renders to
	// output. This allows errors to surface and end things without emitting
	// partial output and then erroring out.

	// If the user wants to render and display the outputs template file then
	// render this. In the event the render returns an error, print this but do
	// not exit. The render can fail due to template function errors, but we
	// can still display the pack templates from above. The error will be
	// displayed before the template renders, so the UI looks OK.
	if c.renderOutputTemplate {
		var outputRender string
		outputRender, err = packManager.ProcessOutputTemplate()
		if err != nil {
			c.ui.ErrorWithContext(err, "failed to render output template", errorContext.GetAll()...)
		} else {
			renders = append(renders, RenderFS{Name: "outputs.tpl", Content: outputRender})
		}
	}

	// Output the renders. Output the files first if enabled so that any renders
	// that display will also have been written to disk.
	for _, render := range renders {
		render.toTerminal(c)
	}

	return 0
}

func (c *RenderFSCommand) Flags() *flag.Sets {
	return c.flagSet(flagSetOperation|flagSetNeedsApproval, func(set *flag.Sets) {
		c.packConfig = &cache.PackConfig{}

		f := set.NewSet("Render Options")

		f.StringVar(&flag.StringVar{
			Name:    "registry",
			Target:  &c.packConfig.Registry,
			Default: "",
			Usage: `Specific registry name containing the pack to be rendered.
					If not specified, the default registry will be used.`,
		})

		f.StringVar(&flag.StringVar{
			Name:    "ref",
			Target:  &c.packConfig.Ref,
			Default: "",
			Usage: `Specific git ref of the pack to be rendered.
					Supports tags, SHA, and latest. If no ref is specified,
					defaults to latest.

					Using ref with a file path is not supported.`,
		})

		f.BoolVar(&flag.BoolVar{
			Name:    "render-output-template",
			Target:  &c.renderOutputTemplate,
			Default: false,
			Usage: `Controls whether or not the output template file within the
					pack is rendered and displayed.`,
		})

		f.BoolVar(&flag.BoolVar{
			Name:    "skip-aux-files",
			Target:  &c.noRenderAuxFiles,
			Default: false,
			Usage: `Controls whether or not the rendered output contains auxiliary
					files found in the 'templates' folder.`,
		})

		f.BoolVar(&flag.BoolVar{
			Name:    "no-format",
			Target:  &c.noFormat,
			Default: false,
			Usage:   `Controls whether or not to format templates before outputting.`,
		})
	})
}

func (c *RenderFSCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *RenderFSCommand) AutocompleteFlags() complete.Flags {
	return c.Flags().Completions()
}

// Help satisfies the Help function of the cli.Command interface.
func (c *RenderFSCommand) Help() string {

	c.Example = `
	# Render an example pack with override variables in a variable file.
	nomad-pack render example --var-file="./overrides.hcl"

	# Render an example pack with cli variable overrides.
	nomad-pack render example --var="redis_image_version=latest" \
		--var="redis_resources={"cpu": "1000", "memory": "512"}"

	# Render an example pack including the outputs template file.
	nomad-pack render example --render-output-template

	# Render an example pack, outputting the rendered templates to file in
	# addition to the terminal. Setting auto-approve allows the command to
	# overwrite existing files.
	nomad-pack render example --to-dir ~/out --auto-approve

	# Render a pack under development from the filesystem - supports current
	# working directory or relative path
	nomad-pack render .
	`

	return formatHelp(`
	Usage: nomad-pack render <pack-name> [options]

	Render the specified Nomad Pack and view the results.

` + c.GetExample() + c.Flags().Help())
}

// Synopsis satisfies the Synopsis function of the cli.Command interface.
func (c *RenderFSCommand) Synopsis() string {
	return "Render the templates within a pack"
}
