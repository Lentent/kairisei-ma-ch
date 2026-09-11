package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type change struct {
	Table    string
	Key      string
	RowID    int64
	UID      int64
	Revision int64
	Digest   string
	Updated  []byte
	Details  []string
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func scan(db queryer, scope map[int64]bool) ([]change, error) {
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, "PRAGMA user_version")
	if err != nil {
		return nil, err
	}
	var version int
	if !rows.Next() {
		rows.Close()
		return nil, fmt.Errorf("数据库缺少版本")
	}
	err = rows.Scan(&version)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if version != 3 {
		return nil, fmt.Errorf("仅支持 schema 3 数据库，当前为 %d；未修改", version)
	}
	out := []change{}
	for _, pair := range [][2]string{{"cn_save_snapshot", "singleton"}, {"cn_account_snapshot", "user_id"}} {
		rows, err := db.QueryContext(ctx, "SELECT "+pair[1]+",revision,payload_json,payload_sha256 FROM "+pair[0]+" ORDER BY "+pair[1])
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			c := change{Table: pair[0], Key: pair[1]}
			var raw []byte
			if err = rows.Scan(&c.RowID, &c.Revision, &raw, &c.Digest); err != nil {
				rows.Close()
				return nil, err
			}
			c.UID = c.RowID
			if c.Table == "cn_save_snapshot" {
				c.UID = 1000001
			}
			if !scope[c.UID] {
				continue
			}
			if digest(raw) != c.Digest {
				rows.Close()
				return nil, fmt.Errorf("UID %d 哈希不符", c.UID)
			}
			var state map[string]json.RawMessage
			if err = json.Unmarshal(raw, &state); err != nil {
				rows.Close()
				return nil, err
			}
			var decks []map[string]json.RawMessage
			if err = json.Unmarshal(state["decks"], &decks); err != nil {
				rows.Close()
				return nil, err
			}
			for _, deck := range decks {
				var ids []int64
				if err = json.Unmarshal(deck["buddy_unique_ids"], &ids); err != nil {
					rows.Close()
					return nil, err
				}
				invalid := false
				for _, id := range ids {
					if id < 0 {
						rows.Close()
						return nil, fmt.Errorf("负数 LE ID，拒绝修改")
					}
				}
				if len(ids) > 0 && ids[0] == 0 {
					for _, id := range ids[1:] {
						invalid = invalid || id != 0
					}
				}
				if !invalid {
					continue
				}
				c.Details = append(c.Details, fmt.Sprintf("职业 %s / 卡组索引 %s：%v -> 全部置零（仅解除编成）", deck["arthur_type"], deck["index"], ids))
				clear(ids)
				deck["buddy_unique_ids"], err = json.Marshal(ids)
				if err != nil {
					rows.Close()
					return nil, err
				}
			}
			if len(c.Details) > 0 {
				state["decks"], err = json.Marshal(decks)
				if err != nil {
					rows.Close()
					return nil, err
				}
				c.Updated, err = json.Marshal(state)
				if err != nil {
					rows.Close()
					return nil, err
				}
				out = append(out, c)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func ask(r *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	s, e := r.ReadString('\n')
	return strings.TrimSpace(s), e
}
func run(r *bufio.Reader) error {
	fmt.Println("LE 编队离线修复工具\n只解除无队长的异常 LE 编成；不会删除 LE、邮件或修改货币。")
	value, err := ask(r, "数据库路径（可拖入文件）：")
	if err != nil {
		return err
	}
	path, err := filepath.Abs(strings.Trim(value, "\""))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("不是数据库文件")
	}
	fmt.Println("目标数据库：", path)
	value, err = ask(r, "请正常停止服务端，修复期间不要启动。输入 STOPPED 确认：")
	if err != nil {
		return err
	}
	if value != "STOPPED" {
		fmt.Println("已取消。")
		return nil
	}
	value, err = ask(r, "要检查的 UID（多个用英文逗号分隔）：")
	if err != nil {
		return err
	}
	scope := map[int64]bool{}
	for _, part := range strings.Split(value, ",") {
		id, e := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if e != nil || id < 1000001 || id >= 1900000000 {
			return fmt.Errorf("UID 无效")
		}
		scope[id] = true
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA busy_timeout=3000"); err != nil {
		return err
	}
	changes, err := scan(db, scope)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Println("所选范围没有此类异常，未修改。")
		return nil
	}
	users := []int64{}
	seen := map[int64]bool{}
	for _, c := range changes {
		fmt.Printf("\nUID %d / %s / 版本 %d\n", c.UID, c.Table, c.Revision)
		for _, d := range c.Details {
			fmt.Println(" ", d)
		}
		if !seen[c.UID] {
			users = append(users, c.UID)
			seen[c.UID] = true
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i] < users[j] })
	labels := []string{}
	for _, id := range users {
		labels = append(labels, strconv.FormatInt(id, 10))
	}
	token := "REPAIR " + strings.Join(labels, ",")
	value, err = ask(r, "\n以上为实际修改范围。二次确认请输入 "+token+"：")
	if err != nil {
		return err
	}
	if value != token {
		fmt.Println("确认不匹配，已取消，未修改。")
		return nil
	}
	backup := path + ".before-le-repair-" + time.Now().UTC().Format("20060102T150405.000000000") + ".bak"
	if _, err = os.Stat(backup); !os.IsNotExist(err) {
		return fmt.Errorf("备份路径已存在或不可访问")
	}
	if _, err = db.Exec("VACUUM INTO ?", backup); err != nil {
		return fmt.Errorf("备份失败：%w", err)
	}
	b, err := sql.Open("sqlite", backup)
	if err != nil {
		return err
	}
	var check string
	err = b.QueryRow("PRAGMA quick_check").Scan(&check)
	if err != nil || check != "ok" {
		b.Close()
		return fmt.Errorf("备份完整性校验失败")
	}
	backed, err := scan(b, scope)
	b.Close()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(backed, changes) {
		return fmt.Errorf("预览后数据发生变化，取消；备份：%s", backup)
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "ROLLBACK")
	current, err := scan(conn, scope)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, changes) {
		return fmt.Errorf("数据已变化，取消，请确认服务端已停止")
	}
	for _, c := range changes {
		result, e := conn.ExecContext(ctx, "UPDATE "+c.Table+" SET payload_json=?,payload_sha256=?,revision=revision+1,updated_utc=? WHERE "+c.Key+"=? AND revision=? AND payload_sha256=?", c.Updated, digest(c.Updated), time.Now().UTC().Format(time.RFC3339Nano), c.RowID, c.Revision, c.Digest)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return fmt.Errorf("更新数量异常，回滚")
		}
		if _, e = conn.ExecContext(ctx, "DELETE FROM cn_account_projection WHERE user_id=?", c.UID); e != nil {
			return e
		}
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	fmt.Println("\n修复完成，账号：", strings.Join(labels, ","))
	fmt.Println("完整数据库备份：", backup)
	fmt.Println("可以启动服务端重新登录。旧服务端仍可能复发，请随后更新修正后的服务端。")
	return nil
}
func main() {
	r := bufio.NewReader(os.Stdin)
	if err := run(r); err != nil {
		fmt.Println("\n修复未完成：", err)
	}
	fmt.Print("\n按回车关闭。")
	_, _ = r.ReadString('\n')
}
