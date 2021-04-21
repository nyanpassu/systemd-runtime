package stdio

// Stdio of a process
type Stdio struct {
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
}

// IsNull returns true if the stdio is not defined
func (s Stdio) IsNull() bool {
	return s.Stdin == "" && s.Stdout == "" && s.Stderr == ""
}
