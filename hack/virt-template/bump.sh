#!/bin/bash

set -ex

TARGET_BRANCH=${1:-"main"}

function latest_version() {
    curl --fail -s "https://api.github.com/repos/kubevirt/virt-template/releases?per_page=100" |
        jq -r '.[] | select(.target_commitish == '\""${TARGET_BRANCH}"\"') | .tag_name' | head -n1
}

function get_digest() {
    local image="$1"
    local tag="$2"
    local arch="$3"

    skopeo inspect --raw "docker://${image}:${tag}" 2>/dev/null |
        jq -r ".manifests[] | select(.platform.architecture == \"${arch}\") | .digest"
}

version=$(latest_version)

if [ -z "${version}" ]; then
    echo "Failed to get latest version"
    exit 1
fi

echo "Bumping to version: ${version}"

# Get digests for virt-template-apiserver
apiserver_amd64=$(get_digest "quay.io/kubevirt/virt-template-apiserver" "${version}" "amd64")
apiserver_arm64=$(get_digest "quay.io/kubevirt/virt-template-apiserver" "${version}" "arm64")
apiserver_s390x=$(get_digest "quay.io/kubevirt/virt-template-apiserver" "${version}" "s390x")

# Get digests for virt-template-controller
controller_amd64=$(get_digest "quay.io/kubevirt/virt-template-controller" "${version}" "amd64")
controller_arm64=$(get_digest "quay.io/kubevirt/virt-template-controller" "${version}" "arm64")
controller_s390x=$(get_digest "quay.io/kubevirt/virt-template-controller" "${version}" "s390x")

# Update default.sh
default_sh="$(dirname "$0")/default.sh"

sed -i "/^[[:blank:]]*virt_template_version[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_VERSION:-\"${version}\"}/" "${default_sh}"

sed -i "/^[[:blank:]]*virt_template_apiserver_digest_amd64[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_APISERVER_DIGEST_AMD64:-\"${apiserver_amd64}\"}/" "${default_sh}"
sed -i "/^[[:blank:]]*virt_template_apiserver_digest_arm64[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_APISERVER_DIGEST_ARM64:-\"${apiserver_arm64}\"}/" "${default_sh}"
sed -i "/^[[:blank:]]*virt_template_apiserver_digest_s390x[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_APISERVER_DIGEST_S390X:-\"${apiserver_s390x}\"}/" "${default_sh}"

sed -i "/^[[:blank:]]*virt_template_controller_digest_amd64[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_CONTROLLER_DIGEST_AMD64:-\"${controller_amd64}\"}/" "${default_sh}"
sed -i "/^[[:blank:]]*virt_template_controller_digest_arm64[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_CONTROLLER_DIGEST_ARM64:-\"${controller_arm64}\"}/" "${default_sh}"
sed -i "/^[[:blank:]]*virt_template_controller_digest_s390x[[:blank:]]*=/s/=.*/=\${VIRT_TEMPLATE_CONTROLLER_DIGEST_S390X:-\"${controller_s390x}\"}/" "${default_sh}"

# Run sync to update deps.bzl and download the manifest
$(dirname "$0")/sync.sh
