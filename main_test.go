package main

import (
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return tt
}

func TestUpdateMarkdown_EmptyFile(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	got := updateMarkdown("", "Overmind Notes", []string{"- 14:27 — first thing"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n\n- 14:27 — first thing\n"
	if got != want {
		t.Errorf("empty file:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_SectionMissing(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\nSome preamble.\n\n## Meetings\n- 10:00 standup\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — bullet"}, now)
	if !strings.Contains(got, "## Overmind Notes\n\n- 14:27 — bullet\n") {
		t.Errorf("expected section to be appended; got:\n%s", got)
	}
	if !strings.HasPrefix(got, in[:len("# 2026-05-18\n\nSome preamble.\n\n## Meetings\n- 10:00 standup")]) {
		t.Errorf("expected existing content preserved; got:\n%s", got)
	}
}

func TestUpdateMarkdown_SectionPresent_InsertBeforeNextHeading(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n- 11:00 — mid\n\n## Tomorrow\n- buy milk\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — new"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n- 11:00 — mid\n- 14:27 — new\n\n## Tomorrow\n- buy milk\n"
	if got != want {
		t.Errorf("want:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_SectionPresent_AtEndOfFile(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — new", "- 14:28 — newer"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n- 14:27 — new\n- 14:28 — newer\n"
	if got != want {
		t.Errorf("want:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_SectionPresent_Empty(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\n## Overmind Notes\n\n## Tomorrow\n- buy milk\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — first"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n- 14:27 — first\n\n## Tomorrow\n- buy milk\n"
	if got != want {
		t.Errorf("want:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_CaseInsensitiveMatch(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "## overmind notes\n- existing\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- new"}, now)
	if !strings.Contains(got, "- existing\n- new") {
		t.Errorf("expected case-insensitive match; got:\n%s", got)
	}
}

func TestFormatBullet(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	if got := formatBullet("  hello  ", now, true); got != "- 14:27 — hello" {
		t.Errorf("with timestamp: got %q", got)
	}
	if got := formatBullet("hello", now, false); got != "- hello" {
		t.Errorf("without timestamp: got %q", got)
	}
}

func TestAssertLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:17310": true,
		"localhost:17310": true,
		"[::1]:17310":     true,
		"0.0.0.0:17310":   false,
		"10.0.0.5:17310":  false,
		"example.com:80":  false,
	}
	for addr, wantOK := range cases {
		err := assertLoopback(addr)
		if (err == nil) != wantOK {
			t.Errorf("assertLoopback(%q): err=%v wantOK=%v", addr, err, wantOK)
		}
	}
}
