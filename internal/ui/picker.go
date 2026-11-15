package ui

// PickOne shows a modal list of choices and returns the index the user
// selected, or ErrCancelled. The This PC "Mount FTP Drive" command uses it
// to choose among unmounted managed drives.
func PickOne(title, prompt, action string, choices []string) (int, error) {
	if len(choices) == 0 {
		return -1, ErrCancelled
	}
	return pickOne(title, prompt, action, choices)
}

// ShowInfo shows an informational message box.
func ShowInfo(title, message string) {
	showInfo(title, message)
}
