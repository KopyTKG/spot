//go:build gui

package ui

import "testing"

// A section a script filled with nothing must not crash the daemon.
func TestChoosingInAnEmptyTileSection(t *testing.T) {
	d, _ := guiDialog(t, "[[items]]\ntype='tiles'\nid='t'\ntiles=[]\n", nil, nil)
	_ = d
	n := &tileNav{groups: []*tileGroup{{id: "t", cols: 1}}}
	n.active = n.groups[0]
	n.choose()
	n.move(0, 1)
	n.move(1, 0)
}
