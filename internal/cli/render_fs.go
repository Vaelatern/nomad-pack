// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/posener/complete"

	"github.com/hashicorp/nomad-pack/internal/pkg/cache"
	"github.com/hashicorp/nomad-pack/internal/pkg/errors"
	"github.com/hashicorp/nomad-pack/internal/pkg/flag"
	"github.com/hashicorp/nomad-pack/internal/pkg/helper"
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

	parsedBuilds map[string]PackEntry

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

type RootDir struct {
	jobs map[string]PackEntry
}

type JobDir struct {
	job PackEntry
}

type PackEntry struct {
	files map[string]string
}

type RootEntry struct {
	conf string
	jobs map[string]PackEntry
}

func (r RenderFS) toTerminal(c *RenderFSCommand) {
	c.ui.Output(r.Name+":", terminal.WithStyle(terminal.BoldStyle))
	c.ui.Output("")
	c.ui.Output(r.Content)
}

func (r RenderFS) toFile(c *RenderFSCommand, ec *errors.UIErrorContext) error {
	return nil
}

func (r RenderFS) Attr(ctx context.Context, attr *fuse.Attr) error {
	// You can fill in some default attributes here if needed
	return nil
}

func (r RenderFS) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	resp.Data = []byte(r.Content)
	return nil
}

func (d *RootDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	job, ok := d.jobs[name]
	if !ok {
		return nil, fuse.Errno(fuse.ENOENT)
	}
	// Return a new node for the job directory
	return &JobDir{job: job}, nil
}

func (d RootDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	var rV []fuse.Dirent
	for nomen, _ := range d.jobs {
		var de fuse.Dirent
		de.Name = nomen
		de.Type = fuse.DT_Dir
		rV = append(rV, de)
	}

	return rV, nil
}

func (d RootDir) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Mode = os.ModeDir | 0o755
	return nil
}

func (d JobDir) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Mode = os.ModeDir | 0o755
	return nil
}

func (j *JobDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	var dirents []fuse.Dirent
	// For each file in the job directory
	for name := range j.job.files {
		de := fuse.Dirent{
			Name: name,
			Type: fuse.DT_File,
		}
		dirents = append(dirents, de)
	}

	return dirents, nil
}

func (r RootEntry) Root() (fs.Node, error) {
	return &RootDir{jobs: r.jobs}, nil
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

	// Build our cancellation context
	ctx, closer := helper.WithInterrupt(context.Background())
	defer closer()

	fp, err := os.Open(c.rootFile)
	if err != nil {
		c.ui.ErrorWithContext(err, ErrParsingArgsOrFlags)
		c.ui.Info(fmt.Sprintf("Failure to open the config file: %v", err))
		return 1
	}
	defer fp.Close()
	fpContents, err := io.ReadAll(fp)
	if err != nil {
		c.ui.ErrorWithContext(err, ErrParsingArgsOrFlags)
		c.ui.Info(fmt.Sprintf("Failure to read the config file: %v", err))
		return 1
	}

	if err := toml.Unmarshal(fpContents, &c.parsedBuilds); err != nil {
		c.ui.ErrorWithContext(err, ErrParsingArgsOrFlags)
		c.ui.Info(fmt.Sprintf("Need a toml file, unmarshal error: %v", err))
		return 1
	}

	fmt.Println(c.parsedBuilds)

	conn, err := fuse.Mount(mountpoint, fuse.ReadOnly(), fuse.FSName("nomad-pack-fs"), fuse.Subtype("packfs"))
	if err != nil {
		c.ui.ErrorWithContext(err, "Failed to mount", errorContext.GetAll()...)
		return 1
	}
	defer conn.Close()
	defer fuse.Unmount(mountpoint)

	err = fs.ServeContext(ctx, conn, RootEntry{conf: c.rootFile, jobs: c.parsedBuilds})
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
	# Render from an example config file to ./mnt
	nomad-pack render-fs example.toml ./mnt
	`

	return formatHelp(`
	Usage: nomad-pack render-fs <pack-config> <mountpoint> [options]

	Render the specified Nomad Pack and view the results.

` + c.GetExample() + c.Flags().Help())
}

// Synopsis satisfies the Synopsis function of the cli.Command interface.
func (c *RenderFSCommand) Synopsis() string {
	return "Render the templates within a pack"
}
