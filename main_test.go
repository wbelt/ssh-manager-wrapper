package main

import (
    "errors"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestListTargets(t *testing.T) {
    dir := t.TempDir()
    _ = os.WriteFile(filepath.Join(dir, "ipa.yaml"), []byte("host: test"), 0644)
    _ = os.WriteFile(filepath.Join(dir, "web.yml"), []byte("host: test"), 0644)
    _ = os.WriteFile(filepath.Join(dir, "notyaml.txt"), []byte("host: test"), 0644)
    _ = os.Mkdir(filepath.Join(dir, "subdir"), 0755)

    targets, err := listTargets(dir)
    assert.NoError(t, err)
    assert.ElementsMatch(t, []string{"ipa", "web"}, targets)
}

func TestBuildSSHArgs(t *testing.T) {
    cfg := HostConfig{
        Host:     "example.com",
        User:     "alice",
        Port:     2222,
        Identity: "/path/to/key",
    }
    cmd := []string{"ls", "-l"}
    args := buildSSHArgs(cfg, cmd)
    assert.Equal(t, []string{"-i", "/path/to/key", "-p", "2222", "alice@example.com", "ls", "-l"}, args)

    cfg = HostConfig{Host: "host", User: "", Port: 22, Identity: ""}
    args = buildSSHArgs(cfg, nil)
    assert.Equal(t, []string{"host"}, args)
}

func TestRenderCommandAndQuoting(t *testing.T) {
    cmd := "ssh"
    args := []string{"-i", "/path/key", "user@host", "echo", "hello world"}
    rendered := renderCommand(cmd, args)
    assert.Contains(t, rendered, "\"hello world\"")
    assert.True(t, needsQuoting("hello world"))
    assert.False(t, needsQuoting("plain"))
    assert.Equal(t, "\"hello world\"", quote("hello world"))
    assert.Equal(t, "\"a\\\"b\"", quote("a\"b"))
    assert.Equal(t, "\"a\\\\b\"", quote("a\\b"))
}

func TestMustWithNil(t *testing.T) {
    assert.NotPanics(t, func() { must(nil) })
}

func TestMustWithError(t *testing.T) {
    defer func() {
        if r := recover(); r == nil {
            t.Errorf("fatal did not exit as expected")
        }
    }()
    oldExit := osExit
    osExit = func(code int) { panic("exit") }
    defer func() { osExit = oldExit }()
    must(errors.New("fail"))
}

