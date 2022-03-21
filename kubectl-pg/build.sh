
VERSION=1.0
sed -i "s/KubectlPgVersion string = \"[^\"]*\"/KubectlPgVersion string = \"${VERSION}\"/" cmd/version.go
go install