FROM golang:1.26-trixie

ARG USERNAME=dev
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN set -xe; \
    groupadd --gid $USER_GID $USERNAME; \
    useradd --uid $USER_UID --gid $USER_GID -s /bin/bash -m $USERNAME; \
    usermod -a -G video $USERNAME; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y sudo; \
    apt-get clean; \
    rm -rf /var/cache/apt/archives /var/lib/apt/lists/*; \
    echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME; \
    chmod 0440 /etc/sudoers.d/$USERNAME; \
    echo "cd \$WORKSPACE_FOLDER" >> /home/dev/.bashrc

RUN set -xe;\
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y \
        clang \
        llvm \
        bpftool \
        libbpf-dev \
    ; \
    apt-get clean; \
    rm -rf /var/cache/apt/archives /var/lib/apt/lists/*;

# [Optional] Set the default user. Omit if you want to keep the default as root.
USER $USERNAME

CMD ["sleep", "infinity"]
