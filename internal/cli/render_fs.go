// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"context"

	"github.com/posener/complete"

	"github.com/hashicorp/nomad-pack/internal/pkg/cache"
	"github.com/hashicorp/nomad-pack/internal/pkg/errors"
	"github.com/hashicorp/nomad-pack/internal/pkg/flag"
	"github.com/hashicorp/nomad-pack/terminal"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	_ "bazil.org/fuse/fs/fstestutil"
	_ "bazil.org/fuse/fuseutil"
)

// RenderFSCommand is a command that allows users to render the templates within
// a pack and see them as a filesystem. This is primarily so you can have a source of
// truth other than your nomad server.
type RenderFSCommand struct {
	*baseCommand
	packConfig *cache.PackConfig

	// rootFile is the essential file we will then use to parse out the builds
	rootFile string

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

type Dir struct {
}

func (d Dir) Attr(ctx context.Context, attr *fuse.Attr) error {
	return nil
}

type RootEntry struct {
	conf string
}

func (r RootEntry) Root() (fs.Node, error) {
	return Dir{}, nil
}

// Run satisfies the Run function of the cli.Command interface.
func (c *RenderFSCommand) Run(args []string) int {
	c.cmdKey = "render-fs" // Add cmdKey here to print out helpUsageMessage on Init error

	if err := c.Init(
		WithExactArgs(2, args),
		WithFlags(c.Flags()),
		WithNoConfig(),
	); err != nil {
		c.ui.ErrorWithContext(err, ErrParsingArgsOrFlags)
		c.ui.Info(c.helpUsageMessage())
		return 1
	}

	errorContext := errors.NewUIErrorContext()

	c.rootFile = c.args[0]

	mountpoint := c.args[1]

	ctx, err := fuse.Mount(mountpoint, fuse.FSName("nomad-pack-fs"), fuse.Subtype("packfs"))
	if err != nil {
		c.ui.ErrorWithContext(err, "Failed to mount", errorContext.GetAll()...)
		return 1
	}
	defer ctx.Close()
	defer fuse.Unmount(mountpoint)

	err = fs.Serve(ctx, RootEntry{conf: "config.yaml"})
	if err != nil {
		c.ui.ErrorWithContext(err, "Failed to mount", errorContext.GetAll()...)
		return 1
	}

	return 0
}

func (c *RenderFSCommand) Flags() *flag.Sets {
	return c.flagSet(flagSetOperation|flagSetNeedsApproval, func(set *flag.Sets) {
		c.packConfig = &cache.PackConfig{}
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
	nomad-pack render-fs example --var-file="./overrides.hcl"

	# Render an example pack with cli variable overrides.
	nomad-pack render-fs example --var="redis_image_version=latest" \
		--var="redis_resources={"cpu": "1000", "memory": "512"}"

	# Render an example pack including the outputs template file.
	nomad-pack render-fs example --render-output-template

	# Render an example pack, outputting the rendered templates to file in
	# addition to the terminal. Setting auto-approve allows the command to
	# overwrite existing files.
	nomad-pack render-fs example --to-dir ~/out --auto-approve

	# Render a pack under development from the filesystem - supports current
	# working directory or relative path
	nomad-pack render-fs .
	`

	return formatHelp(`
	Usage: nomad-pack render-fs <pack-settings> <mountpoint> [options]

	Render the specified Nomad Pack and view the results.

` + c.GetExample() + c.Flags().Help())
}

// Synopsis satisfies the Synopsis function of the cli.Command interface.
func (c *RenderFSCommand) Synopsis() string {
	return "Render the templates within a pack"
}
