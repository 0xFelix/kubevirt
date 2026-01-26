#!/bin/bash

set -ex

source $(dirname "$0")/default.sh

_virt_template_base_url="https://github.com/kubevirt/virt-template/releases/download"
_install_yaml_path="pkg/virt-operator/resource/generate/components/data/virt-template/install-virt-operator.yaml"
_deps_bzl_path="images/virt-template/deps.bzl"

# Download the install manifest
curl \
    -L "${_virt_template_base_url}/${virt_template_version}/install-virt-operator.yaml" \
    -o "${_install_yaml_path}"

# Update deps.bzl with the new digests
cat >"${_deps_bzl_path}" <<EOF
"""Dependencies for virt-template images."""

load("@rules_oci//oci:pull.bzl", "oci_pull")

# Image digests for virt-template components
VIRT_TEMPLATE_APISERVER_DIGESTS = {
    "amd64": "${virt_template_apiserver_digest_amd64}",
    "arm64": "${virt_template_apiserver_digest_arm64}",
    "s390x": "${virt_template_apiserver_digest_s390x}",
}

VIRT_TEMPLATE_CONTROLLER_DIGESTS = {
    "amd64": "${virt_template_controller_digest_amd64}",
    "arm64": "${virt_template_controller_digest_arm64}",
    "s390x": "${virt_template_controller_digest_s390x}",
}

def virt_template_images():
    """Pull virt-template images for all architectures."""
    oci_pull(
        name = "virt_template_apiserver",
        digest = VIRT_TEMPLATE_APISERVER_DIGESTS["amd64"],
        image = "quay.io/kubevirt/virt-template-apiserver",
    )

    oci_pull(
        name = "virt_template_apiserver_aarch64",
        digest = VIRT_TEMPLATE_APISERVER_DIGESTS["arm64"],
        image = "quay.io/kubevirt/virt-template-apiserver",
    )

    oci_pull(
        name = "virt_template_apiserver_s390x",
        digest = VIRT_TEMPLATE_APISERVER_DIGESTS["s390x"],
        image = "quay.io/kubevirt/virt-template-apiserver",
    )

    oci_pull(
        name = "virt_template_controller",
        digest = VIRT_TEMPLATE_CONTROLLER_DIGESTS["amd64"],
        image = "quay.io/kubevirt/virt-template-controller",
    )

    oci_pull(
        name = "virt_template_controller_aarch64",
        digest = VIRT_TEMPLATE_CONTROLLER_DIGESTS["arm64"],
        image = "quay.io/kubevirt/virt-template-controller",
    )

    oci_pull(
        name = "virt_template_controller_s390x",
        digest = VIRT_TEMPLATE_CONTROLLER_DIGESTS["s390x"],
        image = "quay.io/kubevirt/virt-template-controller",
    )
EOF
