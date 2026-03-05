package filetree

import (
	"testing"
)

func TestEntryAtY(t *testing.T) {
	m := Model{
		Entries: []Entry{
			{Name: "file1.txt", Path: "/root/file1.txt", IsDir: false},
			{Name: "file2.txt", Path: "/root/file2.txt", IsDir: false},
			{Name: "file3.txt", Path: "/root/file3.txt", IsDir: false},
		},
		ScrollY: 0,
		Height:  10,
	}

	tests := []struct {
		name      string
		y         int
		wantPath  string
		wantIsDir bool
		wantNil   bool
	}{
		{
			name:      "first entry at y=0",
			y:         0,
			wantPath:  "/root/file1.txt",
			wantIsDir: false,
		},
		{
			name:      "second entry at y=1",
			y:         1,
			wantPath:  "/root/file2.txt",
			wantIsDir: false,
		},
		{
			name:      "third entry at y=2",
			y:         2,
			wantPath:  "/root/file3.txt",
			wantIsDir: false,
		},
		{
			name:    "out of bounds above",
			y:       -1,
			wantNil: true,
		},
		{
			name:    "out of bounds below",
			y:       3,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.EntryAtY(tt.y)
			if tt.wantNil {
				if got != nil {
					t.Errorf("EntryAtY(%d) = %v, want nil", tt.y, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("EntryAtY(%d) = nil, want %q", tt.y, tt.wantPath)
			}
			if got.Path != tt.wantPath {
				t.Errorf("EntryAtY(%d).Path = %q, want %q", tt.y, got.Path, tt.wantPath)
			}
			if got.IsDir != tt.wantIsDir {
				t.Errorf("EntryAtY(%d).IsDir = %v, want %v", tt.y, got.IsDir, tt.wantIsDir)
			}
		})
	}
}

func TestEntryAtYWithScroll(t *testing.T) {
	m := Model{
		Entries: []Entry{
			{Name: "file1.txt", Path: "/root/file1.txt", IsDir: false},
			{Name: "file2.txt", Path: "/root/file2.txt", IsDir: false},
			{Name: "file3.txt", Path: "/root/file3.txt", IsDir: false},
		},
		ScrollY: 1, // scrolled down by 1
		Height:  10,
	}

	tests := []struct {
		name     string
		y        int
		wantPath string
		wantNil  bool
	}{
		{
			name:     "first visible at y=0 with scroll",
			y:        0,
			wantPath: "/root/file2.txt", // scrollY=1, y=0 => idx=1
		},
		{
			name:     "second visible at y=1 with scroll",
			y:        1,
			wantPath: "/root/file3.txt", // scrollY=1, y=1 => idx=2
		},
		{
			name:    "out of bounds with scroll",
			y:       2,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.EntryAtY(tt.y)
			if tt.wantNil {
				if got != nil {
					t.Errorf("EntryAtY(%d) = %v, want nil", tt.y, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("EntryAtY(%d) = nil, want %q", tt.y, tt.wantPath)
			}
			if got.Path != tt.wantPath {
				t.Errorf("EntryAtY(%d).Path = %q, want %q", tt.y, got.Path, tt.wantPath)
			}
		})
	}
}

func TestEntryAtYWithDirectories(t *testing.T) {
	m := Model{
		Entries: []Entry{
			{
				Name:     "dir1",
				Path:     "/root/dir1",
				IsDir:    true,
				Expanded: true,
				Children: []Entry{
					{Name: "child1.txt", Path: "/root/dir1/child1.txt", IsDir: false},
					{Name: "child2.txt", Path: "/root/dir1/child2.txt", IsDir: false},
				},
			},
			{Name: "file2.txt", Path: "/root/file2.txt", IsDir: false},
		},
		ScrollY: 0,
		Height:  10,
	}

	tests := []struct {
		name     string
		y        int
		wantPath string
		wantNil  bool
	}{
		{
			name:     "directory at y=0",
			y:        0,
			wantPath: "/root/dir1",
		},
		{
			name:     "first child at y=1",
			y:        1,
			wantPath: "/root/dir1/child1.txt",
		},
		{
			name:     "second child at y=2",
			y:        2,
			wantPath: "/root/dir1/child2.txt",
		},
		{
			name:     "file after directory at y=3",
			y:        3,
			wantPath: "/root/file2.txt",
		},
		{
			name:    "out of bounds",
			y:       4,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.EntryAtY(tt.y)
			if tt.wantNil {
				if got != nil {
					t.Errorf("EntryAtY(%d) = %v, want nil", tt.y, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("EntryAtY(%d) = nil, want %q", tt.y, tt.wantPath)
			}
			if got.Path != tt.wantPath {
				t.Errorf("EntryAtY(%d).Path = %q, want %q", tt.y, got.Path, tt.wantPath)
			}
		})
	}
}

func TestEntryAtYCollapsedDirectory(t *testing.T) {
	m := Model{
		Entries: []Entry{
			{
				Name:     "dir1",
				Path:     "/root/dir1",
				IsDir:    true,
				Expanded: false, // collapsed
				Children: []Entry{
					{Name: "child1.txt", Path: "/root/dir1/child1.txt", IsDir: false},
				},
			},
			{Name: "file2.txt", Path: "/root/file2.txt", IsDir: false},
		},
		ScrollY: 0,
		Height:  10,
	}

	// When collapsed, children should not be visible
	got := m.EntryAtY(1)
	if got == nil {
		t.Fatal("EntryAtY(1) = nil, want /root/file2.txt")
	}
	if got.Path != "/root/file2.txt" {
		t.Errorf("EntryAtY(1).Path = %q, want /root/file2.txt", got.Path)
	}
}

func TestEntryAtYEmptyTree(t *testing.T) {
	m := Model{
		Entries: []Entry{},
		ScrollY: 0,
		Height:  10,
	}

	got := m.EntryAtY(0)
	if got != nil {
		t.Errorf("EntryAtY(0) = %v, want nil for empty tree", got)
	}

	got = m.EntryAtY(-1)
	if got != nil {
		t.Errorf("EntryAtY(-1) = %v, want nil", got)
	}
}

func TestEntryAtYZeroBasedCoordinates(t *testing.T) {
	m := Model{
		Entries: []Entry{
			{Name: "file1.txt", Path: "/root/file1.txt", IsDir: false},
			{Name: "file2.txt", Path: "/root/file2.txt", IsDir: false},
			{Name: "file3.txt", Path: "/root/file3.txt", IsDir: false},
		},
		ScrollY: 0,
		Height:  10,
	}

	// The app converts terminal's 1-based Y to 0-based:
	//   terminalY - 1 = appY (0-based)
	// Then passes appY directly to EntryAtY
	//
	// Clicking on the 2nd entry in terminal (Y=2 in terminal coords)
	// should map to EntryAtY(1) returning file2.txt

	terminalY := 2        // 1-based terminal coordinate
	appY := terminalY - 1 // Convert to 0-based: appY = 1

	got := m.EntryAtY(appY)
	if got == nil {
		t.Fatalf("EntryAtY(%d) = nil, want /root/file2.txt", appY)
	}
	if got.Path != "/root/file2.txt" {
		t.Errorf("EntryAtY(%d) = %q, want /root/file2.txt", appY, got.Path)
	}

	// Demonstrate the bug: passing terminalY directly would give wrong entry
	gotOldBug := m.EntryAtY(terminalY)
	if gotOldBug != nil {
		t.Logf("Bug demonstration: EntryAtY(%d) = %q (should be file2, got file3)",
			terminalY, gotOldBug.Path)
	}
}
