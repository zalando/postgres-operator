#!/usr/bin/env bash

# Hack to adjust the generated postgresql CRD YAML file and add missing field
# settings which can not be expressed via kubebuilder markers.
#
# Injections:
#
#  * oneOf: for the standby field to enforce that only one of s3_wal_path, gs_wal_path or standby_host is set.
#    * This can later be done with // +kubebuilder:validation:ExactlyOneOf marker, but this requires latest Kubernetes version. (Currently the operator depends on v1.32.9)
#  * type: string and pattern for the maintenanceWindows items.

file="${1:-"manifests/postgresql.crd.yaml"}"

sed -i '/^[[:space:]]*standby:$/{
    # Capture the indentation
    s/^\([[:space:]]*\)standby:$/\1standby:\n\1  oneOf:\n\1  - required:\n\1    - s3_wal_path\n\1  - required:\n\1    - gs_wal_path\n\1  - required:\n\1    - standby_host/
}' "$file"

sed -i '/^[[:space:]]*maintenanceWindows:$/{
    # Capture the indentation
    s/^\([[:space:]]*\)maintenanceWindows:$/\1maintenanceWindows:\n\1  items:\n\1    pattern: '\''^\\ *((Mon|Tue|Wed|Thu|Fri|Sat|Sun):(2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))-((2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))\\ *$'\''\n\1    type: string/
}' "$file"
