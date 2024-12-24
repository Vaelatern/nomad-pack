import re
import subprocess
import tomllib

def render_nomad_pack(pack_path, variables, use_parser_v1=False):
    """
    Renders a Nomad pack using the provided variables and optional `--parser-v1` flag.

    :param pack_path: Path to the Nomad pack directory.
    :param variables: Dictionary of variable names and their values.
    :param use_parser_v1: Whether to include the `--parser-v1` flag.
    :return: Dictionary where keys are filenames and values are file contents.
    :raises: RuntimeError if rendering fails or output cannot be parsed.
    """
    # Build the command
    command = ["nomad-pack", "render", pack_path]
    if use_parser_v1:
        command.append("--parser-v1")
    for key, value in variables.items():
        command.extend(["--var", f"{key}={value}"])

    # Execute the command
    try:
        result = subprocess.run(
            command,
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError as e:
        raise RuntimeError(f"Failed to render Nomad pack: {e}") from e

    # Parse the output
    return parse_rendered_output(result.stdout)

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
            rendered_files = render_nomad_pack(
                pack_path="hello_world",
                variables=job_vars,
                use_parser_v1=parser_v1
            )
            for file_name, content in rendered_files.items():
                print(f"File: {file_name}\nContent:\n{content}\n")
        except Exception as e:
            print(f"Error rendering job {job_name}: {e}")

if __name__ == "__main__":
    # Example usage with the provided TOML configuration file
    toml_file_path = "../example.toml"  # Replace with the actual path to the TOML file
    process_config_and_render(toml_file_path)
