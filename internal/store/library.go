package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

const libraryColumns = `id,COALESCE(parent_id,''),kind,name,description,format,content_sha256,byte_size,token,created_at,updated_at,classification`
const libraryLock int32 = 30

var _ domain.ResearchMaterialRepository = (*Store)(nil)

func scanLibrary(row scanner) (domain.LibraryItem, error) {
	var x domain.LibraryItem
	err := row.Scan(&x.ID, &x.ParentID, &x.Kind, &x.Name, &x.Description, &x.Format, &x.ContentSHA256, &x.ByteSize, &x.Token, &x.CreatedAt, &x.UpdatedAt, &x.Classification)
	return x, databaseError(err)
}
func scanLibraryDetail(row scanner) (domain.LibraryDetail, error) {
	var x domain.LibraryDetail
	err := row.Scan(&x.ID, &x.ParentID, &x.Kind, &x.Name, &x.Description, &x.Format, &x.ContentSHA256, &x.ByteSize, &x.Token, &x.CreatedAt, &x.UpdatedAt, &x.Classification, &x.Content)
	return x, databaseError(err)
}
func libraryToken() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return fmt.Sprintf("%x", b), err
}
func lockLibrary(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, lockNamespace, libraryLock)
	return err
}
func (s *Store) ListLibraryFolders(ctx context.Context) ([]domain.LibraryItem, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return collect(ctx, s.pool, `SELECT `+libraryColumns+` FROM library_items WHERE deleted_at IS NULL AND kind='folder' ORDER BY id LIMIT 256`, scanLibrary)
}
func (s *Store) ListLibraryItems(ctx context.Context, q domain.LibraryQuery) (domain.LibraryPage, error) {
	zero := domain.LibraryPage{Items: []domain.LibraryItem{}}
	if q.ParentID != "" && !domain.ValidLibraryID(q.ParentID) || q.After != "" && !domain.ValidLibraryID(q.After) || q.All && q.ParentID != "" || q.Kind != "" && q.Kind != "file" && q.Kind != "folder" || len(q.Search) > 128 || !utf8.ValidString(q.Search) || strings.ContainsRune(q.Search, 0) || q.Limit < 1 || q.Limit > 100 {
		return zero, invalid("资料筛选或分页无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// position按普通子串搜索，百分号/下划线不升级为SQL通配符。
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.LibraryPage, error) {
		if q.ParentID != "" {
			var kind string
			if err := tx.QueryRow(ctx, `SELECT kind FROM library_items WHERE id=$1 AND deleted_at IS NULL`, q.ParentID).Scan(&kind); err != nil {
				return zero, err
			}
			if kind != "folder" {
				return zero, invalid("目录不是文件夹")
			}
		}
		items, err := collect(ctx, tx, `SELECT `+libraryColumns+` FROM library_items WHERE deleted_at IS NULL AND ($1 OR COALESCE(parent_id,'')=$2) AND ($3='' OR kind=$3) AND ($4='' OR position(lower($4) in lower(name||' '||description))>0) AND id>$5 ORDER BY id LIMIT $6`, scanLibrary, q.All, q.ParentID, q.Kind, q.Search, q.After, q.Limit+1)
		if err != nil {
			return zero, err
		}
		page := domain.LibraryPage{Items: items}
		if len(items) > q.Limit {
			page.Items = items[:q.Limit]
			page.NextCursor = page.Items[len(page.Items)-1].ID
		}
		return page, nil
	})
}
func (s *Store) GetLibraryItem(ctx context.Context, id string) (domain.LibraryDetail, error) {
	if !domain.ValidLibraryID(id) {
		return domain.LibraryDetail{}, invalid("资料ID无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanLibraryDetail(s.pool.QueryRow(ctx, `SELECT `+libraryColumns+`,content FROM library_items WHERE id=$1 AND deleted_at IS NULL`, id))
}
func (s *Store) GetLibraryOperation(ctx context.Context, key string) (domain.LibraryMutation, error) {
	var out domain.LibraryMutation
	if !domain.ValidOperationKey(key) {
		return out, invalid("提交键无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT response FROM library_operations WHERE key=$1`, key).Scan(&raw); err != nil {
		return out, databaseError(err)
	}
	err := json.Unmarshal(raw, &out)
	return out, err
}

// 所有库变更和选用快照都经过同一有界库锁；回执锁先于库锁，正文不入回执/事件。
func (s *Store) mutateLibrary(ctx context.Context, key, id, method string, input any, apply func(pgx.Tx) (domain.LibraryMutation, error)) (domain.LibraryMutation, error) {
	var zero domain.LibraryMutation
	if !domain.ValidLibraryID(id) || !domain.ValidOperationKey(key) {
		return zero, invalid("资料ID或提交键无效")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	path := "/api/library/items/" + id
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.LibraryMutation, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
			return zero, err
		}
		lockHash := sha256.Sum256([]byte("library-operation:" + key))
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, int64(binary.BigEndian.Uint64(lockHash[:8]))); err != nil {
			return zero, err
		}
		var savedMethod, savedPath, savedHash string
		var response []byte
		err := tx.QueryRow(ctx, `SELECT method,path,request_hash,response FROM library_operations WHERE key=$1`, key).Scan(&savedMethod, &savedPath, &savedHash, &response)
		if err == nil {
			if savedMethod != method || savedPath != path || savedHash != hash {
				return zero, conflict("提交键已用于不同资料操作，请核对原提交")
			}
			var saved domain.LibraryMutation
			err = json.Unmarshal(response, &saved)
			return saved, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		if err = lockLibrary(ctx, tx); err != nil {
			return zero, err
		}
		result, err := apply(tx)
		if err != nil {
			return zero, err
		}
		result.Key = key
		result.ItemID = id
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&result.CommittedAt); err != nil {
			return zero, err
		}
		response, err = json.Marshal(result)
		if err != nil {
			return zero, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO library_operations(key,method,path,request_hash,response) VALUES($1,$2,$3,$4,$5)`, key, method, path, hash, response)
		return result, err
	})
}
func (s *Store) SaveLibraryItem(ctx context.Context, key, id string, in domain.LibraryWrite) (domain.LibraryMutation, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := domain.ValidateLibraryWrite(in); err != nil {
		return domain.LibraryMutation{}, err
	}
	return s.mutateLibrary(ctx, key, id, "PUT", in, func(tx pgx.Tx) (domain.LibraryMutation, error) {
		var zero domain.LibraryMutation
		var oldSize int
		var oldKind, oldToken, oldClassification string
		var deleted *time.Time
		err := tx.QueryRow(ctx, `SELECT kind,token,byte_size,deleted_at,classification FROM library_items WHERE id=$1`, id).Scan(&oldKind, &oldToken, &oldSize, &deleted, &oldClassification)
		exists := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		if exists && (deleted != nil || in.ExpectedToken != oldToken || in.Kind != oldKind) || !exists && in.ExpectedToken != "" {
			return zero, conflict("资料已变更或删除，请重新读取后保存")
		}
		if exists && in.ExpectedToken == "" {
			return zero, conflict("资料ID已使用")
		}
		classification := in.Classification
		if classification == "" {
			classification = "material"
			if exists {
				classification = oldClassification
			}
		}
		if classification == "skill" && strings.TrimSpace(in.Description) == "" {
			return zero, invalid("Skill须填写适用说明")
		}
		// 检查目标祖先（每级有界），并保证移动目录后的全部子目录仍在深度上限内。
		depth := 0
		parent := in.ParentID
		visited := map[string]bool{id: true}
		for parent != "" {
			if visited[parent] {
				return zero, invalid("文件夹不能形成循环")
			}
			visited[parent] = true
			depth++
			if depth > domain.MaxLibraryDepth {
				return zero, invalid("文件夹最多8层")
			}
			var kind string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(parent_id,''),kind FROM library_items WHERE id=$1 AND deleted_at IS NULL`, parent).Scan(&parent, &kind); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return zero, conflict("所属目录已删除或不存在")
				}
				return zero, err
			}
			if kind != "folder" {
				return zero, invalid("所属目录必须是文件夹")
			}
		}
		if in.Kind == "folder" {
			depth++
			var below int
			if exists {
				if err := tx.QueryRow(ctx, `WITH RECURSIVE children AS (SELECT id,1 AS d FROM library_items WHERE parent_id=$1 AND kind='folder' AND deleted_at IS NULL UNION ALL SELECT i.id,c.d+1 FROM library_items i JOIN children c ON i.parent_id=c.id WHERE i.kind='folder' AND i.deleted_at IS NULL AND c.d<=8) SELECT COALESCE(max(d),0) FROM children`, id).Scan(&below); err != nil {
					return zero, err
				}
			}
			if depth+below > domain.MaxLibraryDepth {
				return zero, invalid("移动后文件夹超过8层")
			}
		}
		var count, folders, bytes int
		if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE kind='folder'),COALESCE(sum(byte_size),0) FROM library_items WHERE deleted_at IS NULL`).Scan(&count, &folders, &bytes); err != nil {
			return zero, err
		}
		if !exists {
			count++
			if in.Kind == "folder" {
				folders++
			}
		}
		if count > domain.MaxLibraryItems || folders > domain.MaxLibraryFolders || bytes-oldSize+len(in.Content) > domain.MaxLibraryBytes {
			return zero, conflict("资料库容量已满，请先整理资料")
		}
		token, err := libraryToken()
		if err != nil {
			return zero, err
		}
		format, hash := "", ""
		if in.Kind == "file" {
			format = domain.MaterialFormat(in.Name)
			hash = fmt.Sprintf("%x", sha256.Sum256([]byte(in.Content)))
		}
		var item domain.LibraryItem
		if exists {
			item, err = scanLibrary(tx.QueryRow(ctx, `UPDATE library_items SET parent_id=NULLIF($2,''),name=$3,description=$4,format=$5,content=$6,content_sha256=$7,byte_size=$8,token=$9,classification=$10,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+libraryColumns, id, in.ParentID, in.Name, in.Description, format, in.Content, hash, len(in.Content), token, classification))
		} else {
			item, err = scanLibrary(tx.QueryRow(ctx, `INSERT INTO library_items(id,parent_id,kind,name,description,format,content,content_sha256,byte_size,token,classification) VALUES($1,NULLIF($2,''),$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+libraryColumns, id, in.ParentID, in.Kind, in.Name, in.Description, format, in.Content, hash, len(in.Content), token, classification))
		}
		return domain.LibraryMutation{Item: &item}, err
	})
}
func (s *Store) DeleteLibraryItem(ctx context.Context, key, id string, in domain.LibraryDelete) (domain.LibraryMutation, error) {
	if !domain.ValidLibraryID(in.ExpectedToken) {
		return domain.LibraryMutation{}, invalid("资料并发身份无效")
	}
	return s.mutateLibrary(ctx, key, id, "DELETE", in, func(tx pgx.Tx) (domain.LibraryMutation, error) {
		var zero domain.LibraryMutation
		var token string
		if err := tx.QueryRow(ctx, `SELECT token FROM library_items WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&token); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return zero, conflict("资料已删除或不存在")
			}
			return zero, err
		}
		if token != in.ExpectedToken {
			return zero, conflict("资料已变更，请重新读取后删除")
		}
		var children bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM library_items WHERE parent_id=$1 AND deleted_at IS NULL)`, id).Scan(&children); err != nil {
			return zero, err
		}
		if children {
			return zero, conflict("文件夹不为空，请先整理其中资料")
		}
		token, err := libraryToken()
		if err != nil {
			return zero, err
		}
		_, err = tx.Exec(ctx, `UPDATE library_items SET content='',content_sha256='',byte_size=0,description='',parent_id=NULL,token=$2,deleted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, id, token)
		return domain.LibraryMutation{Deleted: true}, err
	})
}
