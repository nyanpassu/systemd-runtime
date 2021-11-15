package systemd

// ShimBinaryName .
const ShimBinaryName = "eru-systemd-shim"

// UnitName .
func UnitName(id string) string {
	return "eru-systemd-" + id
}
