package state

import "testing"

// TestCollectLeafIDs verifies the AppState.CollectLeafIDs helper walks
// nested layout trees correctly.
func TestCollectLeafIDs(t *testing.T) {
	as := AppState{
		Workspaces: []Workspace{
			{
				Layouts: []TabLayout{
					{
						Layout: LayoutNode{
							Type: "split",
							Children: []LayoutNode{
								{Type: "leaf", ID: "leaf-1"},
								{Type: "leaf", ID: "leaf-2"},
								{Type: "split", Children: []LayoutNode{
									{Type: "leaf", ID: "leaf-3"},
								}},
							},
						},
					},
				},
			},
			{
				Layouts: []TabLayout{
					{Layout: LayoutNode{Type: "leaf", ID: "leaf-4"}},
				},
			},
		},
	}

	ids := as.CollectLeafIDs()
	if len(ids) != 4 {
		t.Fatalf("expected 4 leaf ids, got %d", len(ids))
	}
	for _, id := range []string{"leaf-1", "leaf-2", "leaf-3", "leaf-4"} {
		if !ids[id] {
			t.Errorf("expected leaf %q in set", id)
		}
	}
}

// TestCollectLeafIDsEmpty verifies that an empty AppState yields an empty
// (non-nil) map.
func TestCollectLeafIDsEmpty(t *testing.T) {
	as := AppState{}
	ids := as.CollectLeafIDs()
	if ids == nil {
		t.Fatal("expected non-nil map")
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 leaf ids, got %d", len(ids))
	}
}
