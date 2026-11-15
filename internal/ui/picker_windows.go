//go:build windows

package ui

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func pickOne(title, prompt, action string, choices []string) (int, error) {
	var dialog *walk.Dialog
	var list *walk.ListBox
	selected := -1
	accept := func() {
		if list.CurrentIndex() < 0 {
			return
		}
		selected = list.CurrentIndex()
		dialog.Accept()
	}
	_, err := (Dialog{
		AssignTo: &dialog,
		Title:    title,
		MinSize:  Size{Width: 380, Height: 260},
		Layout:   VBox{},
		Children: []Widget{
			Label{Text: prompt},
			ListBox{
				AssignTo:        &list,
				Model:           choices,
				CurrentIndex:    0,
				OnItemActivated: accept,
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{Text: "Cancel", OnClicked: func() { dialog.Cancel() }},
					PushButton{Text: action, OnClicked: accept},
				},
			},
		},
	}).Run(nil)
	if err != nil {
		return -1, err
	}
	if selected < 0 {
		return -1, ErrCancelled
	}
	return selected, nil
}

func showInfo(title, message string) {
	walk.MsgBox(nil, title, message, walk.MsgBoxIconInformation)
}
