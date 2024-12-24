import os
import re
import pty
import subprocess
import struct
import fcntl
import termios
import tomllib

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
                data = os.read(master_fd, 1024).decode()
                if not data:
                    break
                stdout.append(data)
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
        start = match.end()
        end = matches[i + 1].start() if i + 1 < len(matches) else len(output)
        file_content = output[start:end].strip()
        files[file_name] = file_content

    return files

def process_config_and_render(toml_file):
    """
    Processes a TOML configuration file to render multiple jobs from a Nomad pack.

    :param toml_file: Path to the TOML configuration file.
    """
    with open(toml_file, 'rb') as fp:
        config = tomllib.load(fp)
    global_settings = config.get("hello_world", {})
    parser_v1 = global_settings.get("parserV1", False)

    for job_name, job_vars in global_settings.items():
        if job_name == "parserV1":
            continue  # Skip global setting, already handled

        print(f"Rendering job: {job_name}")
        try:
            output = render_nomad_pack_with_pty(
                pack_path="hello_world",
                variables=job_vars,
                use_parser_v1=parser_v1
            )
            rendered_files = parse_rendered_output(output)
            for file_name, content in rendered_files.items():
                print(f"File: {file_name}\nContent:\n{content}\n")
        except Exception as e:
            print(f"Error rendering job {job_name}: {e}")

if __name__ == "__main__":
    # Example usage with the provided TOML configuration file
    toml_file_path = "example.toml"  # Replace with the actual path to the TOML file
    process_config_and_render(toml_file_path)
