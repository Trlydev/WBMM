package nml

import (
	"reflect"
	"testing"
)

func TestParsePatches(t *testing.T) {
	src := `using HarmonyLib;
// [HarmonyPatch(typeof(Commented), "out")]
[HarmonyPatch(typeof(Actor), "getHit")]
class GetHit {
	static bool Prefix(Actor __instance) { return true; }
	static void Postfix() {}
}
[HarmonyPatch(typeof(MapBox))]
static class MapPatches {
	[HarmonyPatch(nameof(MapBox.updateSimulation)), HarmonyPrefix]
	static void Tick() {}
	[HarmonyPatch("clearWorld")]
	[HarmonyTranspiler]
	static IEnumerable<CodeInstruction> Clear(IEnumerable<CodeInstruction> c) => c;
}
class Plain { static bool Prefix() { return false; } }
`
	got := parsePatches(src, "A.cs")
	want := []Patch{
		{"Actor.getHit", "prefix", true, "A.cs"},
		{"Actor.getHit", "postfix", false, "A.cs"},
		{"MapBox.updateSimulation", "prefix", false, "A.cs"},
		{"MapBox.clearWorld", "transpiler", false, "A.cs"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
	ov := Overlaps(map[string][]Patch{"A": got, "B": {{Target: "Actor.getHit", Kind: "prefix", Skips: true}}, "C": {{Target: "Actor.getHit", Kind: "postfix"}}})
	if len(ov) != 1 || ov[0].Target != "Actor.getHit" || !reflect.DeepEqual(ov[0].Mods, []string{"A", "B"}) {
		t.Fatalf("overlaps %+v", ov)
	}
}
