package store

import (
	"errors"
	"testing"

	"wscyber/internal/domain"
)

func TestPostgresLibraryCapacity(t *testing.T) {
	for _, test := range []struct {
		name, sql string
		in        domain.LibraryWrite
	}{
		{"条目", `INSERT INTO library_items(id,kind,name,format,content,content_sha256,byte_size,token) SELECT md5(i::text),'file',i::text||'.txt','text','x',repeat('a',64),1,md5(i::text) FROM generate_series(1,2000) i`, fileInput("新资料.txt", "x")},
		{"文件夹", `INSERT INTO library_items(id,kind,name,format,content,content_sha256,byte_size,token) SELECT md5(i::text),'folder',i::text,'','','',0,md5(i::text) FROM generate_series(1,256) i`, domain.LibraryWrite{Kind: "folder", Name: "新文件夹"}},
		{"总字节", `INSERT INTO library_items(id,kind,name,format,content,content_sha256,byte_size,token) SELECT md5(i::text),'file',i::text||'.txt','text',repeat('x',32768),repeat('a',64),32768,md5(i::text) FROM generate_series(1,512) i`, fileInput("额外.txt", "x")},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			if _, err := s.pool.Exec(ctx, test.sql); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveLibraryItem(ctx, startKey, materialID(9000), test.in); !errors.Is(err, domain.ErrConflict) {
				t.Fatal("达到容量后仍可新增", err)
			}
			if _, err := s.GetLibraryOperation(ctx, startKey); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal("拒绝操作不应保存回执", err)
			}
		})
	}
}
func TestPostgresResearchCorruptionFailsClosed(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	a := saveMaterial(t, s, ctx, 1, fileInput("a.md", "固定原件"))
	started, err := s.StartExploration(ctx, startKey, startInputMaterial(a), true)
	if err != nil {
		t.Fatal(err)
	}
	// 仅隔离测试故意绕过不可变触发器，模拟存储损坏；读取必须失败而非将错误原件交给模型。
	if _, err = s.pool.Exec(ctx, `ALTER TABLE mission_materials DISABLE TRIGGER mission_materials_immutable; UPDATE mission_materials SET content_sha256=repeat('0',64); ALTER TABLE mission_materials ENABLE TRIGGER mission_materials_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetTaskMaterial(ctx, started.Mission.ID, a.ID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = s.GetResearchContext(ctx, started.Mission.ID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
}
