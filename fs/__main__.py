# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "fusepy",
# ]
# ///

import os
import re
import pty
import subprocess
import struct
import fcntl
import termios
import tomllib

from errno import ENOENT
from fuse import FUSE, FuseOSError, Operations

class RenderedFilesystem(Operations):
    def __init__(self, packs):
        self.packs = packs
        self.files = {'/': {}}
        self._packs_process()

    def _packs_process(self):
        for dir, files in self.packs.items():
            self.files['/' + dir] = {}
            for path, contents in files.items():
                if '/' in path:
                    parts = path.split('/')
                    for joined in ['/'.join(parts[:i+1]) for i in range(len(parts)-1)]:
                        self.files[f'/{dir}/{joined}'] = {}
                self.files[f'/{dir}/{path}'] = {'contents': contents}

    def readdir(self, path, fh):
        if fh != 0:
            print(f'filehandle passed to readdir: {path} {fh}')
        returnSet = ['.', '..']
        if path == '/':
            return ['.', '..'] + list(self.packs.keys())
        if path in self.files.keys():
            for p in self.files.keys():
                if not p.startswith(path):
                    continue
                p = p[len(path)+1:] # +1 to remove leading /
                if '/' in p or len(p) == 0:
                    continue
                returnSet += [p]
            return returnSet
        return returnSet

    def getattr(self, path, fh=None):
        if path not  in self.files.keys():
            raise FuseOSError(ENOENT)
        if 'contents' not in self.files[path].keys():
            return {
                'st_mode': 0o40755,
                'st_nlink': 2,
                'st_size': 4096,
            }
        return {
            'st_mode': 0o100644,
            'st_nlink': 2,
            'st_size': len(self.files[path]['contents']),
        }

    def read(self, path, size, offset, fh):
        # Have to encode as utf-8 or fuse might misinterpret as utf32?
        return self.files[path]['contents'][offset:offset+size].encode('utf-8')

    def statfs(self, path):
        return dict(f_bsize=512, f_blocks=4096, f_bavail=2048)


def set_terminal_size(fd, rows, cols):
    """
    Sets the terminal size for the given file descriptor.

    :param fd: File descriptor of the PTY master or slave.
    :param rows: Number of rows (height).
    :param cols: Number of columns (width).
    """
    size = struct.pack("HHHH", rows, cols, 0, 0)  # rows, cols, xpixel, ypixel
    fcntl.ioctl(fd, termios.TIOCSWINSZ, size)

def render_nomad_pack_with_pty(pack_path, variables, use_parser_v1=False):
    """
    Renders a Nomad pack using the provided variables and optional `--parser-v1` flag,
    simulating a real terminal with a pseudo-terminal (PTY).

    :param pack_path: Path to the Nomad pack directory.
    :param variables: Dictionary of variable names and their values.
    :param use_parser_v1: Whether to include the `--parser-v1` flag.
    :return: String output from the `nomad-pack render` command.
    :raises: RuntimeError if rendering fails.
    """
    # Build the command
    command = ["nomad-pack", "render", pack_path]
    if use_parser_v1:
        command.append("--parser-v1")
    for key, value in variables.items():
        command.extend(["--var", f"{key}={value}"])

    # Use a pseudo-terminal to simulate an interactive terminal
    master_fd, slave_fd = pty.openpty()
    set_terminal_size(slave_fd, rows=24, cols=80)

    try:
        process = subprocess.Popen(
            command,
            stdin=subprocess.PIPE,
            stdout=slave_fd,
            stderr=subprocess.PIPE,
            text=True,
        )
        os.close(slave_fd)  # Only the child process writes to the slave end

        # Read output from the master end
        stdout = []
        while True:
            try:
                data = os.read(master_fd, 1024).decode('utf-8', errors='ignore')
                if not data:
                    break
                stdout.extend(data)
            except OSError:
                break
        process.wait()

        if process.returncode != 0:
            stderr = process.stderr.read()
            raise RuntimeError(f"Failed to render Nomad pack: {stderr.strip()}")

        return ''.join(stdout)
    finally:
        os.close(master_fd)

def parse_rendered_output(output):
    """
    Parses the rendered output of nomad-pack into a dictionary of filenames and contents.
    Bold ANSI escape codes are used to detect filenames.

    :param output: String output from `nomad-pack render`.
    :return: Dictionary where keys are filenames and values are file contents.
    """
    bold_regex = re.compile(r'\x1b\[1m(.*?)\x1b\[0m')
    files = {}

    # Find all bold filenames
    matches = list(bold_regex.finditer(output))
    if not matches:
        raise ValueError("No bold filenames found in rendered output")

    # Extract file content for each filename
    for i, match in enumerate(matches):
        file_name = match.group(1)
        if file_name[-1] == ':': # Output puts a colon at end of names for presentation purposes
            file_name = file_name[0:-1]
        start = match.end()
        end = matches[i + 1].start() if i + 1 < len(matches) else len(output)
        file_content = output[start:end].strip()+'\n' # Newline terminate but trim other excess
        files[file_name] = file_content

    return files

def process_config_and_render(toml_file, mountdir):
    """
    Processes a TOML configuration file to render multiple jobs from a Nomad pack.

    :param toml_file: Path to the TOML configuration file.
    """
    with open(toml_file, 'rb') as fp:
        config = tomllib.load(fp)
    rendered = {}
    for pack_name, pack_settings in config.items():
        parser_v1 = pack_settings.get("parserV1", False)

        for job_name, job_vars in pack_settings.items():
            if not isinstance(job_vars, dict):
                continue  # Skip settings

            print(f'Rendering job: {pack_name}:{job_name}')
            try:
                output = render_nomad_pack_with_pty(
                    pack_path=pack_name,
                    variables={"job_name":job_name, **job_vars},
                    use_parser_v1=parser_v1
                )
                rendered_files = parse_rendered_output(output)
            except Exception as e:
                print(f"Error rendering job {job_name}: {e}")
            rendered[f'{pack_name}:{job_name}'] = rendered_files

    FUSE(RenderedFilesystem(rendered), mountdir, foreground=True)

def usage():
    """
    This program is meant to be used to render nomad-pack information to a FUSE mounted fileystem

    The CLI invocation takes two positional arguments:

    :param toml: Path to toml file containing pack instructions
    :param mountpoint: Path to directory to use as mount point
    """
    print(usage.__doc__)


if __name__ == "__main__":
    import sys
    if len(sys.argv) != 3:
        usage()
        exit(1)
    toml_file_path = sys.argv[1]
    mountdir = sys.argv[2]
    process_config_and_render(toml_file_path, mountdir)
