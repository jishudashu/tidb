// Copyright 2021 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package statistics_test

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pingcap/failpoint"
	metamodel "github.com/pingcap/tidb/pkg/meta/model"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/statistics"
	statstestutil "github.com/pingcap/tidb/pkg/statistics/handle/ddl/testutil"
	"github.com/pingcap/tidb/pkg/statistics/handle/types"
	"github.com/pingcap/tidb/pkg/testkit"
	"github.com/pingcap/tidb/pkg/testkit/analyzehelper"
	"github.com/pingcap/tidb/pkg/testkit/testdata"
	"github.com/stretchr/testify/require"
)

func TestChangeVerTo2Behavior(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	originalVal1 := tk.MustQuery("select @@tidb_persist_analyze_options").Rows()[0][0].(string)
	defer func() {
		tk.MustExec(fmt.Sprintf("set global tidb_persist_analyze_options = %v", originalVal1))
	}()
	tk.MustExec("set global tidb_persist_analyze_options=false")

	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int, index idx(a))")
	tk.MustExec("set @@session.tidb_analyze_version = 1")
	tk.MustExec("insert into t values(1, 1), (1, 2), (1, 3)")
	analyzehelper.TriggerPredicateColumnsCollection(t, tk, store, "t", "a", "b")
	tk.MustExec("analyze table t")
	is := dom.InfoSchema()
	tblT, err := is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	h := dom.StatsHandle()
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT := h.GetTableStats(tblT.Meta())
	// Analyze table with version 1 success, all statistics are version 1.
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(1), col.GetStatsVer())
		return false
	})
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
	tk.MustExec("set @@session.tidb_analyze_version = 2")
	tk.MustExec("analyze table t index idx")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead"))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t index")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead"))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t ")
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(2), col.GetStatsVer())
		return false
	})
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(2), idx.GetStatsVer())
		return false
	})
	tk.MustExec("set @@session.tidb_analyze_version = 1")
	tk.MustExec("analyze table t index idx")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead",
		"Warning 1105 The version 2 would collect all statistics not only the selected indexes"))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(2), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t index")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead",
		"Warning 1105 The version 2 would collect all statistics not only the selected indexes"))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(2), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t ")
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(1), col.GetStatsVer())
		return false
	})
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
}

func TestChangeVerTo2BehaviorWithPersistedOptions(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	originalVal1 := tk.MustQuery("select @@tidb_persist_analyze_options").Rows()[0][0].(string)
	defer func() {
		tk.MustExec(fmt.Sprintf("set global tidb_persist_analyze_options = %v", originalVal1))
	}()
	tk.MustExec("set global tidb_persist_analyze_options=true")

	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int, index idx(a))")
	tk.MustExec("set @@session.tidb_analyze_version = 1")
	tk.MustExec("insert into t values(1, 1), (1, 2), (1, 3)")
	analyzehelper.TriggerPredicateColumnsCollection(t, tk, store, "t", "a", "b")
	tk.MustExec("analyze table t")
	is := dom.InfoSchema()
	tblT, err := is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	h := dom.StatsHandle()
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT := h.GetTableStats(tblT.Meta())
	// Analyze table with version 1 success, all statistics are version 1.
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(1), col.GetStatsVer())
		return false
	})
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
	tk.MustExec("set @@session.tidb_analyze_version = 2")
	tk.MustExec("analyze table t index idx")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead"))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t index")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead"))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t ")
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(2), col.GetStatsVer())
		return false
	})
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(2), idx.GetStatsVer())
		return false
	})
	tk.MustExec("set @@session.tidb_analyze_version = 1")
	tk.MustExec("analyze table t index idx")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead",
		"Warning 1105 The version 2 would collect all statistics not only the selected indexes",
		"Note 1105 Analyze use auto adjusted sample rate 1.000000 for table test.t, reason to use this rate is \"use min(1, 110000/3) as the sample-rate=1\"")) // since fallback to ver2 path, should do samplerate adjustment
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(2), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t index")
	tk.MustQuery("show warnings").Check(testkit.Rows("Warning 1105 The analyze version from the session is not compatible with the existing statistics of the table. Use the existing version instead",
		"Warning 1105 The version 2 would collect all statistics not only the selected indexes",
		"Note 1105 Analyze use auto adjusted sample rate 1.000000 for table test.t, reason to use this rate is \"use min(1, 110000/3) as the sample-rate=1\""))
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(2), idx.GetStatsVer())
		return false
	})
	tk.MustExec("analyze table t ")
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT = h.GetTableStats(tblT.Meta())
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(1), col.GetStatsVer())
		return false
	})
	statsTblT.ForEachIndexImmutable(func(_ int64, idx *statistics.Index) bool {
		require.Equal(t, int64(1), idx.GetStatsVer())
		return false
	})
}

func TestExpBackoffEstimation(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec(`set @@tidb_enable_non_prepared_plan_cache=0`) // estRows won't be updated if hit cache.
	tk.MustExec("create table exp_backoff(a int, b int, c int, d int, index idx(a, b, c, d))")
	tk.MustExec("insert into exp_backoff values(1, 1, 1, 1), (1, 1, 1, 2), (1, 1, 2, 3), (1, 2, 2, 4), (1, 2, 3, 5)")
	tk.MustExec("set @@session.tidb_analyze_version=2")
	tk.MustExec("analyze table exp_backoff")
	var (
		input  []string
		output [][]string
	)
	integrationSuiteData := statistics.GetIntegrationSuiteData()
	integrationSuiteData.LoadTestCases(t, &input, &output)
	inputLen := len(input)
	// The test cases are:
	// Query a = 1, b = 1, c = 1, d >= 3 and d <= 5 separately. We got 5, 3, 2, 3.
	// And then query and a = 1 and b = 1 and c = 1 and d >= 3 and d <= 5. It's result should follow the exp backoff,
	// which is 2/5 * (3/5)^{1/2} * (3/5)*{1/4} * 1^{1/8} * 5 = 1.3634.
	for i := range inputLen - 1 {
		testdata.OnRecord(func() {
			output[i] = testdata.ConvertRowsToStrings(tk.MustQuery(input[i]).Rows())
		})
		tk.MustQuery(input[i]).Check(testkit.Rows(output[i]...))
	}

	// The last case is that no column is loaded and we get no stats at all.
	require.NoError(t, failpoint.Enable("github.com/pingcap/tidb/pkg/planner/cardinality/cleanEstResults", `return(true)`))
	testdata.OnRecord(func() {
		output[inputLen-1] = testdata.ConvertRowsToStrings(tk.MustQuery(input[inputLen-1]).Rows())
	})
	tk.MustQuery(input[inputLen-1]).Check(testkit.Rows(output[inputLen-1]...))
	require.NoError(t, failpoint.Disable("github.com/pingcap/tidb/pkg/planner/cardinality/cleanEstResults"))
}

func TestNULLOnFullSampling(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("drop table if exists t;")
	tk.MustExec("set @@session.tidb_analyze_version = 2;")
	tk.MustExec("create table t(a int, index idx(a))")
	tk.MustExec("insert into t values(1), (1), (1), (2), (2), (3), (4), (null), (null), (null)")
	var (
		input  []string
		output [][]string
	)
	tk.MustExec("analyze table t with 2 topn")
	is := dom.InfoSchema()
	tblT, err := is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	h := dom.StatsHandle()
	require.NoError(t, h.Update(context.Background(), is))
	statsTblT := h.GetTableStats(tblT.Meta())
	// Check the null count is 3.
	statsTblT.ForEachColumnImmutable(func(_ int64, col *statistics.Column) bool {
		require.Equal(t, int64(3), col.NullCount)
		return false
	})
	integrationSuiteData := statistics.GetIntegrationSuiteData()
	integrationSuiteData.LoadTestCases(t, &input, &output)
	// Check the topn and buckets contains no null values.
	for i := range input {
		testdata.OnRecord(func() {
			output[i] = testdata.ConvertRowsToStrings(tk.MustQuery(input[i]).Rows())
		})
		tk.MustQuery(input[i]).Check(testkit.Rows(output[i]...))
	}
}

func TestAnalyzeSnapshot(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("drop table if exists t")
	tk.MustExec("set @@session.tidb_analyze_version = 2;")
	tk.MustExec("create table t(a int, index(a))")
	tk.MustExec("insert into t values(1), (1), (1)")
	tk.MustExec("analyze table t")
	rows := tk.MustQuery("select count, snapshot, version from mysql.stats_meta").Rows()
	require.Len(t, rows, 1)
	require.Equal(t, "3", rows[0][0])
	s1Str := rows[0][1].(string)
	s1, err := strconv.ParseUint(s1Str, 10, 64)
	require.NoError(t, err)
	require.True(t, s1 < math.MaxUint64)

	// TestHistogramsWithSameTxnTS
	v1 := rows[0][2].(string)
	rows = tk.MustQuery("select version from mysql.stats_histograms").Rows()
	require.Len(t, rows, 2)
	v2 := rows[0][0].(string)
	require.Equal(t, v1, v2)
	v3 := rows[1][0].(string)
	require.Equal(t, v2, v3)

	tk.MustExec("insert into t values(1), (1), (1)")
	tk.MustExec("analyze table t")
	rows = tk.MustQuery("select count, snapshot from mysql.stats_meta").Rows()
	require.Len(t, rows, 1)
	require.Equal(t, "6", rows[0][0])
	s2Str := rows[0][1].(string)
	s2, err := strconv.ParseUint(s2Str, 10, 64)
	require.NoError(t, err)
	require.True(t, s2 < math.MaxUint64)
	require.True(t, s2 > s1)
}

func TestOutdatedStatsCheck(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)

	oriStart := tk.MustQuery("select @@tidb_auto_analyze_start_time").Rows()[0][0].(string)
	oriEnd := tk.MustQuery("select @@tidb_auto_analyze_end_time").Rows()[0][0].(string)
	statistics.AutoAnalyzeMinCnt = 0
	defer func() {
		statistics.AutoAnalyzeMinCnt = 1000
		tk.MustExec(fmt.Sprintf("set global tidb_auto_analyze_start_time='%v'", oriStart))
		tk.MustExec(fmt.Sprintf("set global tidb_auto_analyze_end_time='%v'", oriEnd))
	}()
	tk.MustExec("set global tidb_auto_analyze_start_time='00:00 +0000'")
	tk.MustExec("set global tidb_auto_analyze_end_time='23:59 +0000'")
	tk.MustExec("set session tidb_enable_pseudo_for_outdated_stats=1")

	h := dom.StatsHandle()
	tk.MustExec("use test")
	tk.MustExec("create table t (a int)")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec("insert into t values (1)" + strings.Repeat(", (1)", 19)) // 20 rows
	analyzehelper.TriggerPredicateColumnsCollection(t, tk, store, "t", "a")
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	is := dom.InfoSchema()
	require.NoError(t, h.Update(context.Background(), is))
	// To pass the stats.Pseudo check in autoAnalyzeTable
	tk.MustExec("analyze table t")
	tk.MustExec("explain select * from t where a = 1")
	require.NoError(t, h.LoadNeededHistograms(dom.InfoSchema()))

	getStatsHealthy := func() int {
		rows := tk.MustQuery("show stats_healthy where db_name = 'test' and table_name = 't'").Rows()
		require.Len(t, rows, 1)
		healthy, err := strconv.Atoi(rows[0][3].(string))
		require.NoError(t, err)
		return healthy
	}

	tk.MustExec("insert into t values (1)" + strings.Repeat(", (1)", 13)) // 34 rows
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	require.Equal(t, getStatsHealthy(), 30)
	require.False(t, hasPseudoStats(tk.MustQuery("explain select * from t where a = 1").Rows()))
	tk.MustExec("insert into t values (1)") // 35 rows
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	require.Equal(t, getStatsHealthy(), 25)
	require.True(t, hasPseudoStats(tk.MustQuery("explain select * from t where a = 1").Rows()))

	tk.MustExec("analyze table t")

	tk.MustExec("delete from t limit 24") // 11 rows
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	require.Equal(t, getStatsHealthy(), 31)
	require.False(t, hasPseudoStats(tk.MustQuery("explain select * from t where a = 1").Rows()))

	tk.MustExec("delete from t limit 1") // 10 rows
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	require.Equal(t, getStatsHealthy(), 28)
	require.True(t, hasPseudoStats(tk.MustQuery("explain select * from t where a = 1").Rows()))
}

func hasPseudoStats(rows [][]any) bool {
	for i := range rows {
		if strings.Contains(rows[i][4].(string), "stats:pseudo") {
			return true
		}
	}
	return false
}

func TestShowHistogramsLoadStatus(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	h := dom.StatsHandle()
	origLease := h.Lease()
	h.SetLease(time.Second)
	defer func() { h.SetLease(origLease) }()
	tk.MustExec("use test")
	tk.MustExec("create table t(a int primary key, b int, c int, index idx(b, c))")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec("insert into t values (1,2,3), (4,5,6)")
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	tk.MustExec("analyze table t")
	require.NoError(t, h.Update(context.Background(), dom.InfoSchema()))
	rows := tk.MustQuery("show stats_histograms where db_name = 'test' and table_name = 't'").Rows()
	for _, row := range rows {
		require.Equal(t, "allEvicted", row[10].(string))
	}
}

func TestSingleColumnIndexNDV(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	h := dom.StatsHandle()
	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int, c varchar(20), d varchar(20), index idx_a(a), index idx_b(b), index idx_c(c), index idx_d(d))")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec("insert into t values (1, 1, 'xxx', 'zzz'), (2, 2, 'yyy', 'zzz'), (1, 3, null, 'zzz')")
	for range 5 {
		tk.MustExec("insert into t select * from t")
	}
	tk.MustExec("analyze table t")
	rows := tk.MustQuery("show stats_histograms where db_name = 'test' and table_name = 't'").Sort().Rows()
	expectedResults := [][]string{
		{"a", "2", "0"}, {"b", "3", "0"}, {"c", "2", "32"}, {"d", "1", "0"},
		{"idx_a", "2", "0"}, {"idx_b", "3", "0"}, {"idx_c", "2", "32"}, {"idx_d", "1", "0"},
	}
	for i, row := range rows {
		require.Equal(t, expectedResults[i][0], row[3]) // column_name
		require.Equal(t, expectedResults[i][1], row[6]) // distinct_count
		require.Equal(t, expectedResults[i][2], row[7]) // null_count
	}
}

func TestColumnStatsLazyLoad(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	h := dom.StatsHandle()
	originLease := h.Lease()
	defer h.SetLease(originLease)
	// Set `Lease` to `Millisecond` to enable column stats lazy load.
	h.SetLease(time.Millisecond)
	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int)")
	tk.MustExec("insert into t values (1,2), (3,4), (5,6), (7,8)")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	analyzehelper.TriggerPredicateColumnsCollection(t, tk, store, "t", "a", "b")
	tk.MustExec("analyze table t")
	is := dom.InfoSchema()
	tbl, err := is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	tblInfo := tbl.Meta()
	c1 := tblInfo.Columns[0]
	c2 := tblInfo.Columns[1]
	require.True(t, h.GetTableStats(tblInfo).GetCol(c1.ID).IsAllEvicted())
	require.True(t, h.GetTableStats(tblInfo).GetCol(c2.ID).IsAllEvicted())
	tk.MustExec("analyze table t")
	require.True(t, h.GetTableStats(tblInfo).GetCol(c1.ID).IsAllEvicted())
	require.True(t, h.GetTableStats(tblInfo).GetCol(c2.ID).IsAllEvicted())
}

func TestUpdateNotLoadIndexFMSketch(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	h := dom.StatsHandle()
	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int, index idx(a)) partition by range (a) (partition p0 values less than (10),partition p1 values less than maxvalue)")
	tk.MustExec("insert into t values (1,2), (3,4), (5,6), (7,8)")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec("analyze table t")
	is := dom.InfoSchema()
	tbl, err := is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	tblInfo := tbl.Meta()
	idxInfo := tblInfo.Indices[0]
	p0 := tblInfo.Partition.Definitions[0]
	p1 := tblInfo.Partition.Definitions[1]
	require.Nil(t, h.GetPartitionStats(tblInfo, p0.ID).GetIdx(idxInfo.ID).FMSketch)
	require.Nil(t, h.GetPartitionStats(tblInfo, p1.ID).GetIdx(idxInfo.ID).FMSketch)
	h.Clear()
	require.NoError(t, h.Update(context.Background(), is))
	require.Nil(t, h.GetPartitionStats(tblInfo, p0.ID).GetIdx(idxInfo.ID).FMSketch)
	require.Nil(t, h.GetPartitionStats(tblInfo, p1.ID).GetIdx(idxInfo.ID).FMSketch)
}

func TestIssue44369(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	h := dom.StatsHandle()
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int, index iab(a,b));")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec("insert into t value(1,1);")
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	tk.MustExec("analyze table t;")
	is := dom.InfoSchema()
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustExec("alter table t rename column b to bb;")
	tk.MustExec("select * from t where a = 10 and bb > 20;")
}

func TestTableLastAnalyzeVersion(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	h := dom.StatsHandle()
	tk := testkit.NewTestKit(t, store)

	// Only create table should not set the last_analyze_version
	tk.MustExec("use test")
	tk.MustExec("create table t(a int);")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	is := dom.InfoSchema()
	require.NoError(t, h.Update(context.Background(), is))
	tbl, err := is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	statsTbl, found := h.Get(tbl.Meta().ID)
	require.True(t, found)
	require.Equal(t, uint64(0), statsTbl.LastAnalyzeVersion)

	// Only alter table should not set the last_analyze_version
	tk.MustExec("alter table t add column b int default 0")
	is = dom.InfoSchema()
	tbl, err = is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	require.NoError(t, err)
	err = statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	require.NoError(t, h.Update(context.Background(), is))
	statsTbl, found = h.Get(tbl.Meta().ID)
	require.True(t, found)
	require.Equal(t, uint64(0), statsTbl.LastAnalyzeVersion)
	tk.MustExec("alter table t add index idx(a)")
	is = dom.InfoSchema()
	tbl, err = is.TableByName(context.Background(), ast.NewCIStr("test"), ast.NewCIStr("t"))
	e := <-h.DDLEventCh()
	require.Equal(t, metamodel.ActionAddIndex, e.GetType())
	require.Equal(t, 0, len(h.DDLEventCh()))
	require.NoError(t, err)
	require.NoError(t, h.Update(context.Background(), is))
	statsTbl, found = h.Get(tbl.Meta().ID)
	require.True(t, found)
	require.Equal(t, uint64(0), statsTbl.LastAnalyzeVersion)

	// INSERT and updating the modify_count should not set the last_analyze_version
	tk.MustExec("insert into t values(1, 1)")
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	statsTbl, found = h.Get(tbl.Meta().ID)
	require.True(t, found)
	require.Equal(t, uint64(0), statsTbl.LastAnalyzeVersion)

	// After analyze, last_analyze_version is set.
	tk.MustExec("analyze table t")
	require.NoError(t, h.Update(context.Background(), is))
	statsTbl, found = h.Get(tbl.Meta().ID)
	require.True(t, found)
	require.NotEqual(t, uint64(0), statsTbl.LastAnalyzeVersion)
}

func TestGlobalIndexWithAnalyzeVersion1AndHistoricalStats(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("set tidb_analyze_version = 1")
	tk.MustExec("set global tidb_enable_historical_stats = true")
	defer tk.MustExec("set global tidb_enable_historical_stats = default")

	tk.MustExec("use test")
	tk.MustExec(`CREATE TABLE t ( a int, b int, c int default 0)
					PARTITION BY RANGE (a) (
					PARTITION p0 VALUES LESS THAN (10),
					PARTITION p1 VALUES LESS THAN (20),
					PARTITION p2 VALUES LESS THAN (30),
					PARTITION p3 VALUES LESS THAN (40))`)
	tk.MustExec("ALTER TABLE t ADD UNIQUE INDEX idx(b) GLOBAL")
	tk.MustExec("INSERT INTO t(a, b) values(1, 1), (2, 2), (3, 3), (15, 15), (25, 25), (35, 35)")

	tblID := dom.MustGetTableID(t, "test", "t")

	for range 10 {
		tk.MustExec("analyze table t")
	}
	// Each analyze will only generate one record
	tk.MustQuery(fmt.Sprintf("select count(*) from mysql.stats_history where table_id=%d", tblID)).Equal(testkit.Rows("10"))
}

func TestLastAnalyzeVersionNotChangedWithAsyncStatsLoad(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("set @@tidb_stats_load_sync_wait = 0;")
	tk.MustExec("use test")
	tk.MustExec("create table t(a int, b int);")
	err := statstestutil.HandleNextDDLEventWithTxn(dom.StatsHandle())
	require.NoError(t, err)
	require.NoError(t, dom.StatsHandle().Update(context.Background(), dom.InfoSchema()))
	tk.MustExec("insert into t values (1, 1);")
	err = dom.StatsHandle().DumpStatsDeltaToKV(true)
	require.NoError(t, err)
	tk.MustExec("alter table t add column c int default 1;")
	err = statstestutil.HandleNextDDLEventWithTxn(dom.StatsHandle())
	require.NoError(t, err)
	tk.MustExec("select * from t where a = 1 or b = 1 or c = 1;")
	require.NoError(t, dom.StatsHandle().LoadNeededHistograms(dom.InfoSchema()))
	result := tk.MustQuery("show stats_meta where table_name = 't'")
	require.Len(t, result.Rows(), 1)
	// The last analyze time.
	require.Equal(t, "<nil>", result.Rows()[0][6])
}

func TestSaveMetaToStorage(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	tableCount := 10
	metaUpdates := make([]types.MetaUpdate, 0, tableCount)
	tableIDs := make([]string, 0, tableCount)
	for i := range tableCount {
		tableName := fmt.Sprintf("save_metas_%d", i)
		tk.MustExec(fmt.Sprintf("drop table if exists test.%s", tableName))
		tk.MustExec(fmt.Sprintf("create table test.%s (id int)", tableName))
		tableInfo, err := dom.InfoSchema().TableInfoByName(ast.NewCIStr("test"), ast.NewCIStr(tableName))
		require.NoError(t, err)
		metaUpdates = append(metaUpdates, types.MetaUpdate{
			PhysicalID: tableInfo.ID,
			Count:      tableInfo.ID,
		})
		tableIDs = append(tableIDs, fmt.Sprintf("%d", tableInfo.ID))
	}
	statsHandler := dom.StatsHandle()
	err := statsHandler.SaveMetaToStorage("test", false, metaUpdates...)
	require.NoError(t, err)
	rows := tk.MustQuery(
		fmt.Sprintf(
			"select version, table_id, modify_count, count, snapshot, last_stats_histograms_version from mysql.stats_meta where table_id in (%s)",
			strings.Join(tableIDs, ","),
		),
	).Rows()
	require.Len(t, rows, tableCount)
	baseVersion := ""
	for _, cols := range rows {
		require.Len(t, cols, 6)
		version := cols[0].(string)
		tableID := cols[1].(string)
		modifyCount := cols[2].(string)
		count := cols[3].(string)
		snapshot := cols[4].(string)
		lastStatsHistogramsVersion := cols[5].(string)
		if len(baseVersion) > 0 {
			require.Equal(t, baseVersion, version)
		} else {
			baseVersion = version
		}
		require.NotEqual(t, "0", tableID)
		require.Equal(t, tableID, count)
		require.Equal(t, "0", snapshot)
		require.Equal(t, "0", modifyCount)
		require.Equal(t, "<nil>", lastStatsHistogramsVersion)
	}

	for i := range tableCount {
		metaUpdates[i].ModifyCount = metaUpdates[i].Count
		metaUpdates[i].Count += metaUpdates[i].ModifyCount
	}
	err = statsHandler.SaveMetaToStorage("test", true, metaUpdates...)
	require.NoError(t, err)
	rows = tk.MustQuery(
		fmt.Sprintf(
			"select version, table_id, modify_count, count, snapshot, last_stats_histograms_version from mysql.stats_meta where table_id in (%s)",
			strings.Join(tableIDs, ","),
		),
	).Rows()
	require.Len(t, rows, tableCount)
	nextVersion := ""
	for _, cols := range rows {
		require.Len(t, cols, 6)
		version := cols[0].(string)
		tableID := cols[1].(string)
		var tableIDI64 int64
		_, err := fmt.Sscanf(tableID, "%d", &tableIDI64)
		require.NoError(t, err)
		expectCount := fmt.Sprintf("%d", tableIDI64*2)
		modifyCount := cols[2].(string)
		count := cols[3].(string)
		snapshot := cols[4].(string)
		lastStatsHistogramsVersion := cols[5].(string)
		if len(nextVersion) > 0 {
			require.Equal(t, nextVersion, version)
		} else {
			nextVersion = version
			require.NotEqual(t, baseVersion, nextVersion)
		}
		require.NotEqual(t, "0", tableID)
		require.Equal(t, expectCount, count)
		require.Equal(t, "0", snapshot)
		require.Equal(t, tableID, modifyCount)
		require.Equal(t, version, lastStatsHistogramsVersion)
	}
}
