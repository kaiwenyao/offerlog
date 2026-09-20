// 导入预检的「疑似重复」必须查库。
//
// 原始 bug：它只在上传的文件内部两两比较，从不问数据库。于是最常走的那条路
// ——导出 CSV → 改几行 → 导回去——预检报「疑似重复 0 行」，确认之后每个岗位都
// 变成两条，而设置页的文案还在说「导入前会做预检（…重复候选）」。
package integration

import (
	"context"
	"testing"

	"offerlog/backend/internal/transfers"
)

func TestImportPreviewFlagsRowsAlreadyInTheDatabase(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	mustCreate(t, svc, owner, "DupCo", "后端工程师")
	mustCreate(t, svc, owner, "OtherCo", "前端")

	tr := transfers.New(db)
	csvData := "公司,岗位\n" +
		"DupCo,后端工程师\n" + // 已经在库里 → 第 2 行
		" dupco , 后端工程师 \n" + // 大小写 / 空白不同，仍是同一个岗位 → 第 3 行
		"NewCo,数据\n" + // 全新 → 不标
		"NewCo,数据\n" // 文件内部自己重复 → 第 5 行

	pv, err := tr.ParseCSV(ctx, owner, "x.csv", stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.Errors) != 0 {
		t.Fatalf("unexpected preview errors: %+v", pv.Errors)
	}
	want := []int64{2, 3, 5}
	if len(pv.DuplicateCandidates) != len(want) {
		t.Fatalf("duplicate_candidates = %v, want lines %v", pv.DuplicateCandidates, want)
	}
	for i, line := range want {
		if pv.DuplicateCandidates[i] != line {
			t.Errorf("duplicate_candidates[%d] = %d, want CSV line %d", i, pv.DuplicateCandidates[i], line)
		}
	}
}

// 另一个人的岗位不算我的重复：去重键是按 owner 查的。
func TestImportPreviewDuplicatesAreScopedToTheOwner(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	mustCreate(t, svc, owner, "ScopedCo", "后端")

	other := createOwner(t, db)
	pv, err := transfers.New(db).ParseCSV(ctx, other, "x.csv", stringsNewReader("公司,岗位\nScopedCo,后端\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.DuplicateCandidates) != 0 {
		t.Errorf("duplicate_candidates = %v, want none (另一个账号的岗位)", pv.DuplicateCandidates)
	}
}

// 回收站里的岗位不算重复：它对用户来说已经不在了，再导一次就是重新建立。
func TestImportPreviewIgnoresTrashedApplications(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "TrashedCo", "后端")
	if err := svc.SoftDelete(ctx, owner, app.ID); err != nil {
		t.Fatal(err)
	}

	pv, err := transfers.New(db).ParseCSV(ctx, owner, "x.csv", stringsNewReader("公司,岗位\nTrashedCo,后端\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pv.DuplicateCandidates) != 0 {
		t.Errorf("duplicate_candidates = %v, want none (已在回收站)", pv.DuplicateCandidates)
	}
}
