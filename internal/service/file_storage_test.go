package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalFSProvider_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	p, err := NewLocalFSProvider(tmp)
	if err != nil {
		t.Fatal(err)
	}

	rel := "generations/2026-04/user-x/task-y.bin"
	want := bytes.Repeat([]byte("payload-"), 100)
	n, err := p.Save(context.Background(), rel, bytes.NewReader(want))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if int(n) != len(want) {
		t.Fatalf("写入 %d 字节，期望 %d", n, len(want))
	}

	r, err := p.Open(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close() // Windows 下不能删除已打开的文件，必须显式关闭
	if !bytes.Equal(got, want) {
		t.Fatal("读出的内容与写入不一致")
	}

	// Stat
	st, err := p.Stat(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size != int64(len(want)) {
		t.Fatalf("Stat.Size = %d, want %d", st.Size, len(want))
	}

	// Delete
	if err := p.Delete(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stat(context.Background(), rel); err == nil || !os.IsNotExist(err) {
		t.Fatal("删除后 Stat 应该 NotExist")
	}

	// Delete 幂等
	if err := p.Delete(context.Background(), rel); err != nil {
		t.Fatalf("Delete 幂等失败: %v", err)
	}
}

func TestLocalFSProvider_PathTraversal(t *testing.T) {
	tmp := t.TempDir()
	p, err := NewLocalFSProvider(tmp)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"../escape.txt",
		"../../escape.txt",
		"..",
	}
	for _, rel := range cases {
		_, err := p.Save(context.Background(), rel, strings.NewReader("x"))
		if err == nil {
			t.Errorf("Save(%q) 应该被拒绝", rel)
		}
	}
}

func TestLocalFSProvider_AtomicSave(t *testing.T) {
	// 测试 Save 失败时不会留下残破文件（rename 之前 .part 应被清理）
	tmp := t.TempDir()
	p, err := NewLocalFSProvider(tmp)
	if err != nil {
		t.Fatal(err)
	}
	rel := "x.bin"
	if _, err := p.Save(context.Background(), rel, &errReader{}); err == nil {
		t.Fatal("应该返回错误")
	}
	if _, err := os.Stat(filepath.Join(tmp, rel)); !os.IsNotExist(err) {
		t.Fatal("失败的 Save 不应该留下文件")
	}
	if _, err := os.Stat(filepath.Join(tmp, rel+".part")); !os.IsNotExist(err) {
		t.Fatal("失败的 Save 不应该留下 .part 文件")
	}
}

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
