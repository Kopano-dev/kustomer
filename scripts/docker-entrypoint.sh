#!/bin/sh
#
# Copyright 2019 Kopano and its licensors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

set -euo pipefail

# Check for parameters, prepend with our exe when the first arg is a parameter.
if [ "${1:0:1}" = '-' ]; then
	set -- ${EXE} "$@"
else
	# Check for some basic commands, this is used to allow easy calling without
	# having to prepend the binary all the time.
	case "${1}" in
		help|version)
			set -- ${EXE} "$@"
			;;

		serve)
			shift
			set -- ${EXE} serve "$@"
			;;
	esac
fi

# Support additional args provided via environment.
if [ -n "${ARGS}" ]; then
	set -- "$@" ${ARGS}
fi

# Run the service, optionally switching user when running as root.
if [ $(id -u) = 0 -a -n "${KUSTOMERD_USER}" ]; then
	userAndgroup="${KUSTOMERD_USER}"
	if [ -n "${KUSTOMERD_GROUP}" ]; then
		userAndgroup="${userAndgroup}:${KUSTOMERD_GROUP}"
	fi
	exec su-exec ${userAndgroup} "$@"
else
	exec "$@"
fi
