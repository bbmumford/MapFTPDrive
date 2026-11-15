//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"mapftpdrive/internal/security"
)

// walkPrompter shows trust prompts owned by a form. Workflows run off the UI
// thread so the window stays responsive during network scans; each prompt
// is therefore marshalled onto the form's thread with Synchronize and the
// caller waits for the answer.
type walkPrompter struct {
	owner walk.Form
}

func (p walkPrompter) ask(title, message string, style walk.MsgBoxStyle) bool {
	if p.owner == nil {
		return walk.MsgBox(nil, title, message, style|walk.MsgBoxYesNo) == walk.DlgCmdYes
	}
	answer := make(chan bool, 1)
	p.owner.Synchronize(func() {
		answer <- walk.MsgBox(p.owner, title, message, style|walk.MsgBoxYesNo|walk.MsgBoxDefButton2) == walk.DlgCmdYes
	})
	return <-answer
}

func (p walkPrompter) inform(title, message string, style walk.MsgBoxStyle) {
	if p.owner == nil {
		walk.MsgBox(nil, title, message, style)
		return
	}
	done := make(chan struct{})
	p.owner.Synchronize(func() {
		walk.MsgBox(p.owner, title, message, style)
		close(done)
	})
	<-done
}

func (p walkPrompter) ConfirmPlainFTP() bool {
	return p.ask("Unencrypted connection", plainFTPPrompt, walk.MsgBoxIconWarning)
}

func (p walkPrompter) ConfirmHostKey(host string, key security.HostKey) bool {
	return p.ask("Verify SFTP host key", hostKeyPrompt(host, key), walk.MsgBoxIconQuestion)
}

func (p walkPrompter) ConfirmHostKeyChange(host string, pinned, presented security.HostKey) bool {
	return p.ask("SFTP host key changed", hostKeyChangePrompt(host, pinned, presented), walk.MsgBoxIconWarning)
}

func (p walkPrompter) ConfirmCertificate(host string, certificate security.Certificate) bool {
	return p.ask("Verify FTPS certificate", certificatePrompt(host, certificate), walk.MsgBoxIconQuestion)
}

func (p walkPrompter) ConfirmCertificateChange(host, pinned string, presented security.Certificate) bool {
	return p.ask("FTPS certificate changed", certificateChangePrompt(host, pinned, presented), walk.MsgBoxIconWarning)
}
