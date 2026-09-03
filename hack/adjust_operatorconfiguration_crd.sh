#!/usr/bin/env bash

# Hack to adjust the generated operatorconfiguration CRD YAML file and add
# missing field settings which can not be expressed via kubebuilder markers.
#
# Injections:
#
#  * type: string and pattern for the maintenance_windows items.
#    The MaintenanceWindow type marshals to/from a string (see marshal.go) but
#    the field is declared `+kubebuilder:validation:Schemaless` /
#    `+kubebuilder:validation:Type=array`, so controller-gen emits a bare
#    `type: array` without the required `items` schema. A structural CRD must
#    specify `items` for every array, so the generated CRD is rejected with:
#      spec.validation.openAPIV3Schema.properties[configuration]
#      .properties[maintenance_windows].items: Required value: must be specified

file="${1:-"manifests/operatorconfiguration.crd.yaml"}"

sed -i '/^[[:space:]]*maintenance_windows:$/{
    # Capture the indentation
    s/^\([[:space:]]*\)maintenance_windows:$/\1maintenance_windows:\n\1  items:\n\1    pattern: '\''^\\ *((Mon|Tue|Wed|Thu|Fri|Sat|Sun):(2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))-((2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))\\ *$'\''\n\1    type: string/
}' "$file"
