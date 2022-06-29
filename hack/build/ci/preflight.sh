#!/usr/bin/env bash

readonly PREFLIGHT_VERSION="${1}"
readonly IMAGE_TAR_PATH="${2}"
#readonly API_TOKEN="${3}"
readonly PREFLIGHT_VERSION="${1}"
readonly PREFLIGHT_EXECUTABLE="preflight-linux-amd64"

docker load --input "${IMAGE_TAR_PATH}"
docker image list

exit 0

curl -O "https://github.com/redhat-openshift-ecosystem/openshift-preflight/releases/download/${PREFLIGHT_VERSION}/${PREFLIGHT_EXECUTABLE}"
sudo chmod +x "${PREFLIGHT_EXECUTABLE}"

./preflight check container ????? > >(tee -a report.json) 2> >(tee -a preflight.log >&2)

grep "Preflight result: PASSED" preflight.log || exit 1

#./preflight check container ${{ inputs.image_name }} --submit --pyxis-api-token=<API-Token> --certification-project-id=<projectid from the whole project different one than the image!> --docker-config=./docker-config.json
