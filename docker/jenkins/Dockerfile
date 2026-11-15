# Jenkins for this project: the Go toolchain and the Docker client are baked
# in, so a pipeline can test the code and build both images (portal and n8n)
# through the host's Docker socket. Plugins, the admin user and the pipeline
# job are set up from casc.yaml, so there is no setup wizard.
FROM golang:1.25-bookworm AS go
FROM docker:27-cli AS docker

FROM jenkins/jenkins:lts-jdk21
USER root
COPY --from=go /usr/local/go /usr/local/go
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker
RUN apt-get update && apt-get install -y --no-install-recommends git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/local/go/bin:$PATH \
    GOPATH=/var/jenkins_home/go \
    GOCACHE=/var/jenkins_home/.cache/go-build \
    JAVA_OPTS="-Djenkins.install.runSetupWizard=false" \
    CASC_JENKINS_CONFIG=/usr/share/jenkins/ref/casc.yaml

COPY plugins.txt /usr/share/jenkins/ref/plugins.txt
RUN jenkins-plugin-cli --plugin-file /usr/share/jenkins/ref/plugins.txt
COPY casc.yaml /usr/share/jenkins/ref/casc.yaml
# Runs as root so it can use the mounted Docker socket (local development).
