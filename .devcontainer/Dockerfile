# Use the official Golang image as a base image.
# The version should ideally match the one used in the project.
FROM golang:1.23

# Set up a non-root user for security.
# The 'vscode' user is a standard for dev containers.
RUN useradd -ms /bin/bash vscode

# Install git, which is required for 'go mod' and other development tasks.
RUN apt-get update && apt-get install -y git python3

# Switch to the non-root user.
USER vscode

# Set the working directory.
WORKDIR /workspace

# The command is not strictly necessary as the dev container will override it,
# but it's good practice to have a default command.
CMD ["/bin/bash"]
