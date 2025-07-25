// Copyright 2020 PingCAP, Inc.
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

package executor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pingcap/failpoint"
	"github.com/pingcap/kvproto/pkg/keyspacepb"
	"github.com/pingcap/kvproto/pkg/kvrpcpb"
	"github.com/pingcap/tidb/pkg/config"
	"github.com/pingcap/tidb/pkg/domain/infosync"
	"github.com/pingcap/tidb/pkg/meta/model"
	"github.com/pingcap/tidb/pkg/parser/auth"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"github.com/pingcap/tidb/pkg/sessionctx/variable"
	statstestutil "github.com/pingcap/tidb/pkg/statistics/handle/ddl/testutil"
	"github.com/pingcap/tidb/pkg/store/mockstore"
	"github.com/pingcap/tidb/pkg/testkit"
	"github.com/pingcap/tidb/pkg/testkit/testfailpoint"
	"github.com/pingcap/tidb/pkg/util/logutil"
	"github.com/stretchr/testify/require"
	"github.com/tikv/client-go/v2/tikv"
	"github.com/tikv/client-go/v2/tikvrpc"
	"go.uber.org/zap"
)

func TestInspectionTables(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	instances := []string{
		"pd,127.0.0.1:11080,127.0.0.1:10080,mock-version,mock-githash,0",
		"tidb,127.0.0.1:11080,127.0.0.1:10080,mock-version,mock-githash,1001",
		"tikv,127.0.0.1:11080,127.0.0.1:10080,mock-version,mock-githash,0",
		"tiproxy,127.0.0.1:6000,127.0.0.1:3380,mock-version,mock-githash,0",
		"ticdc,127.0.0.1:8300,127.0.0.1:8301,mock-version,mock-githash,0",
		"tso,127.0.0.1:3379,127.0.0.1:3379,mock-version,mock-githash,0",
		"scheduling,127.0.0.1:4379,127.0.0.1:4379,mock-version,mock-githash,0",
	}
	fpName := "github.com/pingcap/tidb/pkg/infoschema/mockClusterInfo"
	fpExpr := `return("` + strings.Join(instances, ";") + `")`
	require.NoError(t, failpoint.Enable(fpName, fpExpr))
	defer func() { require.NoError(t, failpoint.Disable(fpName)) }()

	tk.MustQuery("select type, instance, status_address, version, git_hash, server_id from information_schema.cluster_info").Check(testkit.Rows(
		"pd 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 0",
		"tidb 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 1001",
		"tikv 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 0",
		"tiproxy 127.0.0.1:6000 127.0.0.1:3380 mock-version mock-githash 0",
		"ticdc 127.0.0.1:8300 127.0.0.1:8301 mock-version mock-githash 0",
		"tso 127.0.0.1:3379 127.0.0.1:3379 mock-version mock-githash 0",
		"scheduling 127.0.0.1:4379 127.0.0.1:4379 mock-version mock-githash 0",
	))

	// enable inspection mode
	inspectionTableCache := map[string]variable.TableSnapshot{}
	tk.Session().GetSessionVars().InspectionTableCache = inspectionTableCache
	tk.MustQuery("select type, instance, status_address, version, git_hash, server_id from information_schema.cluster_info").Check(testkit.Rows(
		"pd 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 0",
		"tidb 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 1001",
		"tikv 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 0",
		"tiproxy 127.0.0.1:6000 127.0.0.1:3380 mock-version mock-githash 0",
		"ticdc 127.0.0.1:8300 127.0.0.1:8301 mock-version mock-githash 0",
		"tso 127.0.0.1:3379 127.0.0.1:3379 mock-version mock-githash 0",
		"scheduling 127.0.0.1:4379 127.0.0.1:4379 mock-version mock-githash 0",
	))
	require.NoError(t, inspectionTableCache["cluster_info"].Err)
	require.Len(t, inspectionTableCache["cluster_info"].Rows, 7)

	// check whether is obtain data from cache at the next time
	inspectionTableCache["cluster_info"].Rows[0][0].SetString("modified-pd", mysql.DefaultCollationName)
	tk.MustQuery("select type, instance, status_address, version, git_hash, server_id from information_schema.cluster_info").Check(testkit.Rows(
		"modified-pd 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 0",
		"tidb 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 1001",
		"tikv 127.0.0.1:11080 127.0.0.1:10080 mock-version mock-githash 0",
		"tiproxy 127.0.0.1:6000 127.0.0.1:3380 mock-version mock-githash 0",
		"ticdc 127.0.0.1:8300 127.0.0.1:8301 mock-version mock-githash 0",
		"tso 127.0.0.1:3379 127.0.0.1:3379 mock-version mock-githash 0",
		"scheduling 127.0.0.1:4379 127.0.0.1:4379 mock-version mock-githash 0",
	))
	tk.Session().GetSessionVars().InspectionTableCache = nil
}

func TestUserPrivileges(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	// test the privilege of new user for information_schema.table_constraints
	tk.MustExec("create user constraints_tester")
	constraintsTester := testkit.NewTestKit(t, store)
	constraintsTester.MustExec("use information_schema")
	require.NoError(t, constraintsTester.Session().Auth(&auth.UserIdentity{
		Username: "constraints_tester",
		Hostname: "127.0.0.1",
	}, nil, nil, nil))
	constraintsTester.MustQuery("select * from information_schema.TABLE_CONSTRAINTS WHERE TABLE_NAME != 'CLUSTER_SLOW_QUERY';").Check([][]any{})

	// test the privilege of user with privilege of mysql.gc_delete_range for information_schema.table_constraints
	tk.MustExec("CREATE ROLE r_gc_delete_range ;")
	tk.MustExec("GRANT ALL PRIVILEGES ON mysql.gc_delete_range TO r_gc_delete_range;")
	tk.MustExec("GRANT r_gc_delete_range TO constraints_tester;")
	constraintsTester.MustExec("set role r_gc_delete_range")
	rows := constraintsTester.MustQuery("select * from information_schema.TABLE_CONSTRAINTS where TABLE_NAME='gc_delete_range';").Rows()
	require.Greater(t, len(rows), 0)
	constraintsTester.MustQuery("select * from information_schema.TABLE_CONSTRAINTS where TABLE_NAME='tables_priv';").Check([][]any{})

	// test the privilege of new user for information_schema
	tk.MustExec("create user tester1")
	tk1 := testkit.NewTestKit(t, store)
	tk1.MustExec("use information_schema")
	require.NoError(t, tk1.Session().Auth(&auth.UserIdentity{
		Username: "tester1",
		Hostname: "127.0.0.1",
	}, nil, nil, nil))
	tk1.MustQuery("select * from information_schema.STATISTICS WHERE TABLE_NAME != 'CLUSTER_SLOW_QUERY';").Check([][]any{})

	// test the privilege of user with some privilege for information_schema
	tk.MustExec("create user tester2")
	tk.MustExec("CREATE ROLE r_columns_priv;")
	tk.MustExec("GRANT ALL PRIVILEGES ON mysql.columns_priv TO r_columns_priv;")
	tk.MustExec("GRANT r_columns_priv TO tester2;")
	tk2 := testkit.NewTestKit(t, store)
	tk2.MustExec("use information_schema")
	require.NoError(t, tk2.Session().Auth(&auth.UserIdentity{
		Username: "tester2",
		Hostname: "127.0.0.1",
	}, nil, nil, nil))
	tk2.MustExec("set role r_columns_priv")
	rows = tk2.MustQuery("select * from information_schema.STATISTICS where TABLE_NAME='columns_priv' and COLUMN_NAME='Host';").Rows()
	require.Greater(t, len(rows), 0)
	tk2.MustQuery("select * from information_schema.STATISTICS where TABLE_NAME='tables_priv' and COLUMN_NAME='Host';").Check(
		[][]any{})

	// test the privilege of user with all privilege for information_schema
	tk.MustExec("create user tester3")
	tk.MustExec("CREATE ROLE r_all_priv;")
	tk.MustExec("GRANT ALL PRIVILEGES ON mysql.* TO r_all_priv;")
	tk.MustExec("GRANT r_all_priv TO tester3;")
	tk3 := testkit.NewTestKit(t, store)
	tk3.MustExec("use information_schema")
	require.NoError(t, tk3.Session().Auth(&auth.UserIdentity{
		Username: "tester3",
		Hostname: "127.0.0.1",
	}, nil, nil, nil))
	tk3.MustExec("set role r_all_priv")
	rows = tk3.MustQuery("select * from information_schema.STATISTICS where TABLE_NAME='columns_priv' and COLUMN_NAME='Host';").Rows()
	require.Greater(t, len(rows), 0)
	rows = tk3.MustQuery("select * from information_schema.STATISTICS where TABLE_NAME='tables_priv' and COLUMN_NAME='Host';").Rows()
	require.Greater(t, len(rows), 0)
}

func TestDataForTableStatsField(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	h := dom.StatsHandle()
	h.Clear()
	is := dom.InfoSchema()
	tk := testkit.NewTestKit(t, store)

	tk.MustExec("use test")
	tk.MustExec("drop table if exists t")
	tk.MustExec("create table t (c int, d int, e char(5), index idx(e))")
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("0 0 0 0"))
	tk.MustExec(`insert into t(c, d, e) values(1, 2, "c"), (2, 3, "d"), (3, 4, "e")`)
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("3 16 48 0"))
	tk.MustExec(`insert into t(c, d, e) values(4, 5, "f")`)
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("4 16 64 0"))
	tk.MustExec("delete from t where c >= 3")
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("2 16 32 0"))
	tk.MustExec("delete from t where c=3")
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("2 16 32 0"))
	tk.MustExec("analyze table t all columns")
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("2 18 36 4"))

	// Test partition table.
	tk.MustExec("drop table if exists t")
	tk.MustExec(`CREATE TABLE t (a int, b int, c varchar(5), primary key(a), index idx(c)) PARTITION BY RANGE (a) (PARTITION p0 VALUES LESS THAN (6), PARTITION p1 VALUES LESS THAN (11), PARTITION p2 VALUES LESS THAN (16))`)
	err = statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec(`insert into t(a, b, c) values(1, 2, "c"), (7, 3, "d"), (12, 4, "e")`)
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("3 16 48 0"))
	tk.MustExec("analyze table t all columns")
	tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.tables where table_name='t'").Check(
		testkit.Rows("3 18 54 6"))
}

func TestPartitionsTable(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	h := dom.StatsHandle()
	h.Clear()
	is := dom.InfoSchema()

	tk := testkit.NewTestKit(t, store)
	tk.MustExec("USE test;")
	testkit.WithPruneMode(tk, variable.Static, func() {
		tk.MustExec("DROP TABLE IF EXISTS `test_partitions`;")
		tk.MustExec(`CREATE TABLE test_partitions (a int, b int, c varchar(5), primary key(a), index idx(c)) PARTITION BY RANGE (a) (PARTITION p0 VALUES LESS THAN (6), PARTITION p1 VALUES LESS THAN (11), PARTITION p2 VALUES LESS THAN (16));`)
		err := statstestutil.HandleNextDDLEventWithTxn(h)
		require.NoError(t, err)
		tk.MustExec(`insert into test_partitions(a, b, c) values(1, 2, "c"), (7, 3, "d"), (12, 4, "e");`)

		tk.MustQuery("select PARTITION_NAME, PARTITION_DESCRIPTION from information_schema.PARTITIONS where table_name='test_partitions';").Check(
			testkit.Rows("p0 6", "p1 11", "p2 16"))

		tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.PARTITIONS where table_name='test_partitions';").Check(
			testkit.Rows(
				"0 0 0 0",
				"0 0 0 0",
				"0 0 0 0",
			),
		)
		require.NoError(t, h.DumpStatsDeltaToKV(true))
		require.NoError(t, h.Update(context.Background(), is))
		tk.MustQuery("select table_rows, avg_row_length, data_length, index_length from information_schema.PARTITIONS where table_name='test_partitions';").Check(
			testkit.Rows(
				"1 16 16 0",
				"1 16 16 0",
				"1 16 16 0",
			),
		)
	})

	// Test for table has no partitions.
	tk.MustExec("DROP TABLE IF EXISTS `test_partitions_1`;")
	tk.MustExec(`CREATE TABLE test_partitions_1 (a int, b int, c varchar(5), primary key(a), index idx(c));`)
	err := statstestutil.HandleNextDDLEventWithTxn(h)
	require.NoError(t, err)
	tk.MustExec(`insert into test_partitions_1(a, b, c) values(1, 2, "c"), (7, 3, "d"), (12, 4, "e");`)
	require.NoError(t, h.DumpStatsDeltaToKV(true))
	require.NoError(t, h.Update(context.Background(), is))
	tk.MustQuery("select PARTITION_NAME, TABLE_ROWS, AVG_ROW_LENGTH, DATA_LENGTH, INDEX_LENGTH from information_schema.PARTITIONS where table_name='test_partitions_1';").Check(
		testkit.Rows("<nil> 3 16 48 0"))

	tk.MustExec("DROP TABLE IF EXISTS `test_partitions`;")
	tk.MustExec(`CREATE TABLE test_partitions1 (id int, b int, c varchar(5), primary key(id), index idx(c)) PARTITION BY RANGE COLUMNS(id) (PARTITION p0 VALUES LESS THAN (6), PARTITION p1 VALUES LESS THAN (11), PARTITION p2 VALUES LESS THAN (16));`)
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions1';").Check(testkit.Rows("p0 RANGE COLUMNS `id`", "p1 RANGE COLUMNS `id`", "p2 RANGE COLUMNS `id`"))
	tk.MustExec("DROP TABLE test_partitions1")

	tk.MustExec(`CREATE TABLE test_partitions (id int, b int, c varchar(5), primary key(id,b), index idx(c)) PARTITION BY RANGE COLUMNS(id,b) (PARTITION p0 VALUES LESS THAN (6,1), PARTITION p1 VALUES LESS THAN (11,9), PARTITION p2 VALUES LESS THAN (16,MAXVALUE))`)
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions';").Check(testkit.Rows("p0 RANGE COLUMNS `id`,`b`", "p1 RANGE COLUMNS `id`,`b`", "p2 RANGE COLUMNS `id`,`b`"))
	tk.MustExec("DROP TABLE test_partitions")

	tk.MustExec(`create table test_partitions (a varchar(255), b int, c datetime) partition by list columns (b,a) (partition p0 values in ((1,"1"), (3,"3")), partition p1 values in ((2, "2")))`)
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions'").Check(testkit.Rows("p0 LIST COLUMNS `b`,`a`", "p1 LIST COLUMNS `b`,`a`"))
	tk.MustExec("drop table test_partitions")

	tk.MustExec("create table test_partitions (a varchar(3)) partition by list columns (a) (partition p0 values in ('1'), partition p1 values in ('2'))")
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions'").Check(testkit.Rows("p0 LIST COLUMNS `a`", "p1 LIST COLUMNS `a`"))
	tk.MustExec("drop table test_partitions")

	tk.MustExec("create table test_partitions (a int) partition by list (a) (partition p0 values in (1), partition p1 values in (2));")
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions';").Check(testkit.Rows("p0 LIST `a`", "p1 LIST `a`"))
	tk.MustExec("drop table test_partitions")

	tk.MustExec("create table test_partitions (a date) partition by list (year(a)) (partition p0 values in (1), partition p1 values in (2));")
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions';").Check(testkit.Rows("p0 LIST YEAR(`a`)", "p1 LIST YEAR(`a`)"))
	tk.MustExec("drop table test_partitions")

	tk.MustExec("create table test_partitions (a bigint, b date) partition by list columns (a,b) (partition p0 values in ((1,'2020-09-28'),(1,'2020-09-29')));")
	tk.MustQuery("select PARTITION_NAME,PARTITION_METHOD,PARTITION_EXPRESSION from information_schema.partitions where table_name = 'test_partitions';").Check(testkit.Rows("p0 LIST COLUMNS `a`,`b`"))
	pid, err := strconv.Atoi(tk.MustQuery("select TIDB_PARTITION_ID from information_schema.partitions where table_name = 'test_partitions';").Rows()[0][0].(string))
	require.NoError(t, err)
	require.Greater(t, pid, 0)
	tk.MustExec("drop table test_partitions")
}

func TestForAnalyzeStatus(t *testing.T) {
	store, dom := testkit.CreateMockStoreAndDomain(t)
	tk := testkit.NewTestKit(t, store)
	analyzeStatusTable := "CREATE TABLE `ANALYZE_STATUS` (\n" +
		"  `TABLE_SCHEMA` varchar(64) DEFAULT NULL,\n" +
		"  `TABLE_NAME` varchar(64) DEFAULT NULL,\n" +
		"  `PARTITION_NAME` varchar(64) DEFAULT NULL,\n" +
		"  `JOB_INFO` longtext DEFAULT NULL,\n" +
		"  `PROCESSED_ROWS` bigint(21) unsigned DEFAULT NULL,\n" +
		"  `START_TIME` datetime DEFAULT NULL,\n" +
		"  `END_TIME` datetime DEFAULT NULL,\n" +
		"  `STATE` varchar(64) DEFAULT NULL,\n" +
		"  `FAIL_REASON` longtext DEFAULT NULL,\n" +
		"  `INSTANCE` varchar(512) DEFAULT NULL,\n" +
		"  `PROCESS_ID` bigint(21) unsigned DEFAULT NULL,\n" +
		"  `REMAINING_SECONDS` varchar(512) DEFAULT NULL,\n" +
		"  `PROGRESS` double(22,6) DEFAULT NULL,\n" +
		"  `ESTIMATED_TOTAL_ROWS` bigint(21) unsigned DEFAULT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin"
	tk.MustQuery("show create table information_schema.analyze_status").Check(testkit.Rows("ANALYZE_STATUS " + analyzeStatusTable))
	tk.MustExec("delete from mysql.analyze_jobs")
	tk.MustExec("use test")
	tk.MustExec("drop table if exists analyze_test")
	tk.MustExec("create table analyze_test (a int, b int, index idx(a))")
	tk.MustExec("insert into analyze_test values (1,2),(3,4)")

	tk.MustQuery("select distinct TABLE_NAME from information_schema.analyze_status where TABLE_NAME='analyze_test'").Check([][]any{})
	tk.MustExec("analyze table analyze_test all columns")
	tk.MustQuery("select distinct TABLE_NAME from information_schema.analyze_status where TABLE_NAME='analyze_test'").Check(testkit.Rows("analyze_test"))

	// test the privilege of new user for information_schema.analyze_status
	tk.MustExec("create user analyze_tester")
	analyzeTester := testkit.NewTestKit(t, store)
	analyzeTester.MustExec("use information_schema")
	require.NoError(t, analyzeTester.Session().Auth(&auth.UserIdentity{
		Username: "analyze_tester",
		Hostname: "127.0.0.1",
	}, nil, nil, nil))
	analyzeTester.MustQuery("show analyze status").Check([][]any{})
	analyzeTester.MustQuery("select * from information_schema.ANALYZE_STATUS;").Check([][]any{})

	// test the privilege of user with privilege of test.t1 for information_schema.analyze_status
	tk.MustExec("create table t1 (a int, b int, index idx(a))")
	tk.MustExec("insert into t1 values (1,2),(3,4)")
	tk.MustExec("analyze table t1 all columns")
	tk.MustQuery("show warnings").Check(testkit.Rows("Note 1105 Analyze use auto adjusted sample rate 1.000000 for table test.t1, reason to use this rate is \"use min(1, 110000/10000) as the sample-rate=1\"")) // 1 note.
	require.NoError(t, dom.StatsHandle().LoadNeededHistograms(dom.InfoSchema()))
	tk.MustExec("CREATE ROLE r_t1 ;")
	tk.MustExec("GRANT ALL PRIVILEGES ON test.t1 TO r_t1;")
	tk.MustExec("GRANT r_t1 TO analyze_tester;")
	analyzeTester.MustExec("set role r_t1")
	rows := tk.MustQuery("select * from information_schema.analyze_status where TABLE_NAME='t1'").Sort().Rows()
	require.Greater(t, len(rows), 0)
	for _, row := range rows {
		require.Len(t, row, 14) // test length of row
		// test `End_time` field
		str, ok := row[6].(string)
		require.True(t, ok)
		_, err := time.Parse(time.DateTime, str)
		require.NoError(t, err)
	}
	rows2 := tk.MustQuery("show analyze status where TABLE_NAME='t1'").Sort().Rows()
	require.Equal(t, len(rows), len(rows2))
	for i, row := range rows {
		for j, r := range row {
			require.Equal(t, r, rows2[i][j])
		}
	}
}

func TestForServersInfo(t *testing.T) {
	globalCfg := config.GetGlobalConfig()
	t.Cleanup(func() {
		config.StoreGlobalConfig(globalCfg)
	})
	newCfg := *globalCfg
	newCfg.Labels = map[string]string{"dc": "dc1"}
	config.StoreGlobalConfig(&newCfg)

	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	ctx := context.Background()
	sql := "select * from information_schema.TIDB_SERVERS_INFO"
	comment := fmt.Sprintf("sql:%s", sql)
	rs, err := tk.ExecWithContext(ctx, sql)
	require.NoError(t, err)
	require.NotNil(t, rs)
	fields := rs.Fields()
	require.Len(t, fields, 8)
	require.Equal(t, fields[0].ColumnAsName.L, "ddl_id")
	require.Equal(t, fields[1].ColumnAsName.L, "ip")
	require.Equal(t, fields[2].ColumnAsName.L, "port")
	require.Equal(t, fields[3].ColumnAsName.L, "status_port")
	require.Equal(t, fields[4].ColumnAsName.L, "lease")
	require.Equal(t, fields[5].ColumnAsName.L, "version")
	require.Equal(t, fields[6].ColumnAsName.L, "git_hash")
	require.Equal(t, fields[7].ColumnAsName.L, "labels")

	res := tk.ResultSetToResultWithCtx(ctx, rs, comment)
	rows := res.Rows()
	require.Len(t, rows, 1)
	require.Len(t, rows[0], 8)

	info, err := infosync.GetServerInfo()
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, info.ID, rows[0][0])
	require.Equal(t, info.IP, rows[0][1])
	require.Equal(t, strconv.FormatInt(int64(info.Port), 10), rows[0][2])
	require.Equal(t, strconv.FormatInt(int64(info.StatusPort), 10), rows[0][3])
	require.Equal(t, info.Lease, rows[0][4])
	require.Equal(t, info.Version, rows[0][5])
	require.Equal(t, info.GitHash, rows[0][6])
	require.Equal(t, "dc=dc1", rows[0][7])
}

func TestTiFlashSystemTableWithTiFlashV620(t *testing.T) {
	instances := []string{
		"tiflash,127.0.0.1:3933,127.0.0.1:7777,,",
		"tikv,127.0.0.1:11080,127.0.0.1:10080,,",
	}
	fpName := "github.com/pingcap/tidb/pkg/infoschema/mockStoreServerInfo"
	fpExpr := `return("` + strings.Join(instances, ";") + `")`
	require.NoError(t, failpoint.Enable(fpName, fpExpr))
	defer func() { require.NoError(t, failpoint.Disable(fpName)) }()

	mocker := newGetTiFlashSystemTableRequestMocker(t)
	mocker.MockQuery(`SELECT * FROM system.dt_segments LIMIT 0, 1024`, func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error) {
		require.EqualValues(t, req.Sql, "SELECT * FROM system.dt_segments LIMIT 0, 1024")
		data, err := os.ReadFile("testdata/tiflash_v620_dt_segments.json")
		require.NoError(t, err)
		return &kvrpcpb.TiFlashSystemTableResponse{
			Data: data,
		}, nil
	})
	mocker.MockQuery(`SELECT * FROM system.dt_tables LIMIT 0, 1024`, func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error) {
		require.EqualValues(t, req.Sql, "SELECT * FROM system.dt_tables LIMIT 0, 1024")
		data, err := os.ReadFile("testdata/tiflash_v620_dt_tables.json")
		require.NoError(t, err)
		return &kvrpcpb.TiFlashSystemTableResponse{
			Data: data,
		}, nil
	})

	// When try to parse the result from old version tiflash instance with the latest columns name, some columns may not exist.
	// The missing columns will be filled with <nil> value.
	store := testkit.CreateMockStore(t, withMockTiFlash(1), mocker.AsOpt())
	tk := testkit.NewTestKit(t, store)
	tk.MustQuery("select * from information_schema.TIFLASH_SEGMENTS;").Check(testkit.Rows(
		"mysql tables_priv 10 0 1 [-9223372036854775808,9223372036854775807) <nil> 0 0 <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 0 <nil> 2032 <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 127.0.0.1:3933",
		"mysql db 8 0 1 [-9223372036854775808,9223372036854775807) <nil> 0 0 <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 0 <nil> 2032 <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 127.0.0.1:3933",
		"test segment 70 0 1 [01,FA) <nil> 30511 50813627 0.6730359542460096 <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 3578860 <nil> 409336 <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> <nil> 127.0.0.1:3933",
	))
	tk.MustQuery("show warnings").Check(testkit.Rows())

	tk.MustQuery("select * from information_schema.TIFLASH_TABLES;").Check(testkit.Rows(
		"mysql tables_priv 10 0 <nil> 1 0 0 0 <nil> 0 <nil> 0 <nil> <nil> <nil> 0 0 0 0 0 0 <nil> <nil> <nil> 0 0 0 0 <nil> <nil> 0 <nil> <nil> <nil> <nil> 0 <nil> <nil> <nil> 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"mysql db 8 0 <nil> 1 0 0 0 <nil> 0 <nil> 0 <nil> <nil> <nil> 0 0 0 0 0 0 <nil> <nil> <nil> 0 0 0 0 <nil> <nil> 0 <nil> <nil> <nil> <nil> 0 <nil> <nil> <nil> 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"test segment 70 0 <nil> 1 102000 169873868 0 0 0 <nil> 0 <nil> <nil> <nil> 0 102000 169873868 0 0 0 <nil> <nil> <nil> 1 102000 169873868 43867622 102000 169873868 0 <nil> <nil> <nil> <nil> 13 13 7846.153846153846 13067220.615384616 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
	))
	tk.MustQuery("show warnings").Check(testkit.Rows())
}

func TestTiFlashSystemTableWithTiFlashV630(t *testing.T) {
	instances := []string{
		"tiflash,127.0.0.1:3933,127.0.0.1:7777,,",
		"tikv,127.0.0.1:11080,127.0.0.1:10080,,",
	}
	fpName := "github.com/pingcap/tidb/pkg/infoschema/mockStoreServerInfo"
	fpExpr := `return("` + strings.Join(instances, ";") + `")`
	require.NoError(t, failpoint.Enable(fpName, fpExpr))
	defer func() { require.NoError(t, failpoint.Disable(fpName)) }()

	mocker := newGetTiFlashSystemTableRequestMocker(t)
	mocker.MockQuery(`SELECT * FROM system.dt_segments LIMIT 0, 1024`, func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error) {
		require.EqualValues(t, req.Sql, "SELECT * FROM system.dt_segments LIMIT 0, 1024")
		data, err := os.ReadFile("testdata/tiflash_v630_dt_segments.json")
		require.NoError(t, err)
		return &kvrpcpb.TiFlashSystemTableResponse{
			Data: data,
		}, nil
	})

	// When try to parse the result from old version tiflash instance with the latest columns name, some columns may not exist.
	// The missing columns will be filled with <nil> value.
	store := testkit.CreateMockStore(t, withMockTiFlash(1), mocker.AsOpt())
	tk := testkit.NewTestKit(t, store)
	tk.MustQuery("select * from information_schema.TIFLASH_SEGMENTS;").Check(testkit.Rows(
		"mysql tables_priv 10 0 1 [-9223372036854775808,9223372036854775807) 0 0 0 <nil> 0 0 0 0 2 0 0 0 0 0 <nil> 2032 3 0 0 1 1 0 0 0 0 127.0.0.1:3933",
		"test segment 70 436272981189328904 1 [01,FA) 5 102000 169874232 0 0 0 0 0 2 0 0 0 0 0 <nil> 2032 3 102000 169874232 1 68 102000 169874232 43951837 20 127.0.0.1:3933",
		"test segment 75 0 1 [01,013130303030393535FF61653666642D6136FF61382D343032382DFF616436312D663736FF3062323736643461FF3600000000000000F8) 2 0 0 <nil> 0 0 1 1 110 0 0 4 4 0 <nil> 2032 111 0 0 1 70 0 0 0 0 127.0.0.1:3933",
		"test segment 75 0 113 [013130303030393535FF61653666642D6136FF61382D343032382DFF616436312D663736FF3062323736643461FF3600000000000000F8,013139393938363264FF33346535382D3735FF31382D343661612DFF626235392D636264FF3139333434623736FF3100000000000000F9) 2 10167 16932617 0.4887380741615029 0 0 0 0 114 4969 8275782 2 0 0 <nil> 63992 112 5198 8656835 1 71 5198 8656835 2254100 1 127.0.0.1:3933",
		"test segment 75 0 116 [013139393938363264FF33346535382D3735FF31382D343661612DFF626235392D636264FF3139333434623736FF3100000000000000F9,013330303131383034FF61323537662D6638FF63302D346466622DFF383235632D353361FF3236306338616662FF3400000000000000F8) 3 8 13322 0.5 3 4986 1 0 117 1 1668 4 3 4986 <nil> 2032 115 4 6668 1 78 4 6668 6799 1 127.0.0.1:3933",
		"test segment 75 0 125 [013330303131383034FF61323537662D6638FF63302D346466622DFF383235632D353361FF3236306338616662FF3400000000000000F8,013339393939613861FF30663062332D6537FF32372D346234642DFF396535632D363865FF3336323066383431FF6300000000000000F9) 2 8677 14451079 0.4024432407514118 3492 5816059 3 0 126 0 0 0 0 5816059 <nil> 2032 124 5185 8635020 1 79 5185 8635020 2247938 1 127.0.0.1:3933",
		"test segment 75 0 128 [013339393939613861FF30663062332D6537FF32372D346234642DFF396535632D363865FF3336323066383431FF6300000000000000F9,013730303031636230FF32663330652D3539FF62352D346134302DFF613539312D383930FF6132316364633466FF3200000000000000F8) 0 1 1668 1 0 0 0 0 129 1 1668 5 4 0 <nil> 2032 127 0 0 1 78 4 6668 6799 1 127.0.0.1:3933",
		"test segment 75 0 119 [013730303031636230FF32663330652D3539FF62352D346134302DFF613539312D383930FF6132316364633466FF3200000000000000F8,013739393939386561FF36393566612D3534FF64302D346437642DFF383136612D646335FF6432613130353533FF3200000000000000F9) 2 10303 17158730 0.489372027564787 0 0 0 0 120 5042 8397126 2 0 0 <nil> 63992 118 5261 8761604 1 77 5261 8761604 2280506 1 127.0.0.1:3933",
		"test segment 75 0 122 [013739393939386561FF36393566612D3534FF64302D346437642DFF383136612D646335FF6432613130353533FF3200000000000000F9,FA) 0 1 1663 1 0 0 0 0 123 1 1663 4 3 0 <nil> 2032 121 0 0 1 78 4 6668 6799 1 127.0.0.1:3933",
	))
	tk.MustQuery("show warnings").Check(testkit.Rows())
}

func TestTiFlashSystemTableWithTiFlashV640(t *testing.T) {
	instances := []string{
		"tiflash,127.0.0.1:3933,127.0.0.1:7777,,",
		"tikv,127.0.0.1:11080,127.0.0.1:10080,,",
	}
	fpName := "github.com/pingcap/tidb/pkg/infoschema/mockStoreServerInfo"
	fpExpr := `return("` + strings.Join(instances, ";") + `")`
	require.NoError(t, failpoint.Enable(fpName, fpExpr))
	defer func() { require.NoError(t, failpoint.Disable(fpName)) }()

	mocker := newGetTiFlashSystemTableRequestMocker(t)
	mocker.MockQuery(`SELECT * FROM system.dt_tables LIMIT 0, 1024`, func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error) {
		require.EqualValues(t, req.Sql, "SELECT * FROM system.dt_tables LIMIT 0, 1024")
		data, err := os.ReadFile("testdata/tiflash_v640_dt_tables.json")
		require.NoError(t, err)
		return &kvrpcpb.TiFlashSystemTableResponse{
			Data: data,
		}, nil
	})

	// When try to parse the result from old version tiflash instance with the latest columns name, some columns may not exist.
	// The missing columns will be filled with <nil> value.
	store := testkit.CreateMockStore(t, withMockTiFlash(1), mocker.AsOpt())
	tk := testkit.NewTestKit(t, store)
	tk.MustQuery("select * from information_schema.TIFLASH_TABLES;").Check(testkit.Rows(
		"tpcc customer 135 0 <nil> 4 3528714 2464079200 0 0.002329177144988231 1 0 929227 <nil> 0.16169850346757514 0 8128 882178.5 616019800 4 8219 5747810 2054.75 1436952.5 0 4 3520495 2458331390 1601563417 880123.75 614582847.5 24 8 6 342.4583333333333 239492.08333333334 482 120.5 7303.9315352697095 5100272.593360996 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc district 137 0 <nil> 1 7993 1346259 0 0.8748905292130614 1 0.8055198055198055 252168 <nil> 0.21407121407121407 0 147272 7993 1346259 1 6993 1178050 6993 1178050 0 1 1000 168209 91344 1000 168209 6 6 6 1165.5 196341.66666666666 10 10 100 16820.9 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc history 139 0 <nil> 19 19379697 1629276978 0 0.0006053758219233252 0.5789473684210527 0.4626662120695534 253640 <nil> 0.25434708489601093 0 293544 1019984.052631579 85751419.89473684 11 11732 997220 1066.5454545454545 90656.36363636363 0 19 19367965 1628279758 625147717 1019366.5789473684 85698934.63157895 15 4 1.3636363636363635 782.1333333333333 66481.33333333333 2378 125.15789473684211 8144.644659377628 684726.559293524 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc item 141 0 <nil> 1 100000 10799081 0 0 0 <nil> 0 <nil> <nil> <nil> 0 100000 10799081 0 0 0 <nil> <nil> <nil> 1 100000 10799081 7357726 100000 10799081 0 0 <nil> <nil> <nil> 13 13 7692.307692307692 830698.5384615385 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc new_order 143 0 <nil> 4 2717707 78813503 0 0.02266763856442214 1 0.9678592299201351 52809 <nil> 0.029559768846178818 0 1434208 679426.75 19703375.75 4 61604 1786516 15401 446629 0 3 2656103 77026987 40906492 885367.6666666666 25675662.333333332 37 24 9.25 1664.972972972973 48284.21621621621 380 126.66666666666667 6989.744736842105 202702.59736842106 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc order_line 145 0 <nil> 203 210607202 20007684190 0 0.0054566462546708164 0.5862068965517241 0.7810067620424135 620065 <nil> 0.005679558722564825 0 22607144 1037473.9014778325 98560020.64039409 119 1149209 109174855 9657.218487394957 917435.756302521 0 203 209457993 19898509335 8724002804 1031812.7733990147 98022213.47290641 893 39 7.504201680672269 1286.9081746920492 122256.27659574468 31507 155.20689655172413 6647.982765734599 631558.3627447869 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc orders 147 0 <nil> 22 21903301 1270391458 0 0.02021357420052804 0.7272727272727273 0.9239944527763222 260536 <nil> 0.010145817899282655 0 10025264 995604.5909090909 57745066.27272727 16 442744 25679152 27671.5 1604947 0 22 21460557 1244712306 452173775 975479.8636363636 56577832.09090909 242 34 15.125 1829.5206611570247 106112.19834710743 2973 135.13636363636363 7218.485368314833 418672.15136226034 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc stock 149 0 <nil> 42 11112720 4811805131 0 0.028085203262567582 0.9761904761904762 0.8463391893060944 10227093 <nil> 0.07567373591410528 0 6719064 264588.5714285714 114566788.83333333 41 312103 135131097 7612.268292682927 3295880.4146341463 0 42 10800617 4676674034 3231872509 257157.54761904763 111349381.76190476 238 26 5.804878048780488 1311.357142857143 567777.718487395 1644 39.142857142857146 6569.718369829684 2844692.234793187 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
		"tpcc warehouse 151 0 <nil> 1 5842 923615 0 0.9828825744608011 1 0.9669104841518634 70220 <nil> 0.07732497387669801 0 133048 5842 923615 1 5742 907807 5742 907807 0 1 100 15808 11642 100 15808 5 5 5 1148.4 181561.4 5 5 20 3161.6 0 0 0  0 0 0  0 0 0  0 127.0.0.1:3933",
	))
	tk.MustQuery("show warnings").Check(testkit.Rows())
}

func TestTablesTable(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)

	type tableMeta struct {
		schema string
		table  string
		id     string
	}
	toString := func(tm *tableMeta) string {
		return fmt.Sprintf("%s %s %s", tm.schema, tm.table, tm.id)
	}

	// prepare data
	tableMetas := []*tableMeta{}
	schemaNames := []string{"db1", "db2"}
	tableNames := []string{"t1", "t2"}
	tk.MustExec("create database db1")
	tk.MustExec("create database db2")
	for _, schemaName := range schemaNames {
		for _, tableName := range tableNames {
			tk.MustExec(fmt.Sprintf("create table %s.%s (a int)", schemaName, tableName))
			res := tk.MustQuery(fmt.Sprintf("select tidb_table_id from information_schema.tables where table_schema = '%s' and table_name = '%s'", schemaName, tableName))
			// [db1 t1 id0, db1 t2 id1, db2 t1 id2, db2 t2 id3]
			tableMetas = append(tableMetas, &tableMeta{schema: schemaName, table: tableName, id: res.String()})
		}
	}

	// test table mode
	tk.MustQuery(`select tidb_table_mode from information_schema.tables where table_schema = 'db1' and
		table_name = 't1'`).Check(testkit.Rows("Normal"))

	// Predicates are extracted in CNF, so we separate the test cases by the number of disjunctions in the predicate.

	// predicate covers one disjunction
	tk.MustQuery(`select table_schema, table_name, tidb_table_id from information_schema.tables
		where table_schema = 'db1'`).Sort().Check(testkit.Rows(toString(tableMetas[0]), toString(tableMetas[1])))
	tk.MustQuery(`select table_schema, table_name, tidb_table_id from information_schema.tables
		where table_name = 't2'`).Sort().Check(testkit.Rows(toString(tableMetas[1]), toString(tableMetas[3])))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where tidb_table_id = %s", tableMetas[2].id)).Check(
		testkit.Rows(toString(tableMetas[2])))

	// cover two disjunctions
	tk.MustQuery(`select table_schema, table_name, tidb_table_id from information_schema.tables
		where table_schema = 'db1' and table_name = 't2'`).Check(testkit.Rows(toString(tableMetas[1])))
	tk.MustQuery(`select table_schema, table_name, tidb_table_id from information_schema.tables
		where table_schema in ('db1', 'db2') and table_name = 't2'`).Sort().Check(testkit.Rows(toString(tableMetas[1]), toString(tableMetas[3])))
	tk.MustQuery(`select table_schema, table_name, tidb_table_id from information_schema.tables
		where (table_schema = 'db1' or table_schema = 'db2' ) and table_name = 't2'`).Sort().Check(testkit.Rows(toString(tableMetas[1]), toString(tableMetas[3])))
	tk.MustQuery(`select table_schema, table_name, tidb_table_id from information_schema.tables
		where (table_schema = 'db1' or table_schema = 'db2' ) and table_name = 't3'`).Check(testkit.Rows())
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'db1' and tidb_table_id = %s", tableMetas[0].id)).Check(
		testkit.Rows(toString(tableMetas[0])))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_name = 't2' and tidb_table_id = %s", tableMetas[1].id)).Check(
		testkit.Rows(toString(tableMetas[1])))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'db2' and tidb_table_id = %s", tableMetas[1].id)).Check(
		testkit.Rows())

	// cover three disjunctions
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'db1' and table_name = 't1' and tidb_table_id = %s", tableMetas[0].id)).Check(
		testkit.Rows(toString(tableMetas[0])))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'db1' and table_name = 't1' and tidb_table_id = %s", tableMetas[1].id)).Check(
		testkit.Rows())
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'db1' and table_name = 't1' and tidb_table_id in (%s,%s)", tableMetas[0].id, tableMetas[1].id)).Check(
		testkit.Rows(toString(tableMetas[0])))

	selectTables, err := strconv.Atoi(tk.MustQuery("select count(*) from information_schema.tables where upper(table_name) = 'T1'").Rows()[0][0].(string))
	require.NoError(t, err)
	totalTables, err := strconv.Atoi(tk.MustQuery("select count(*) from information_schema.tables").Rows()[0][0].(string))
	require.NoError(t, err)
	remainTables, err := strconv.Atoi(tk.MustQuery("select count(*) from information_schema.tables where upper(table_name) != 'T1'").Rows()[0][0].(string))
	require.NoError(t, err)
	require.Equal(t, 2, selectTables)
	require.Equal(t, totalTables, remainTables+selectTables)
}

func TestColumnTable(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table tbl1(col_1 int primary key, col_2 int, col_4 int);")
	tk.MustExec("create table tbl2(col_1 int primary key, col_2 int, col_3 int);")
	tk.MustExec("create view view1 as select min(col_1), col_2, max(col_4) as max4 from tbl1 group by col_2;")

	tk.MustQuery("select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns where TABLE_SCHEMA = 'test';").Sort().Check(
		testkit.RowsWithSep("|",
			"test|tbl1|col_1",
			"test|tbl1|col_2",
			"test|tbl1|col_4",
			"test|tbl2|col_1",
			"test|tbl2|col_2",
			"test|tbl2|col_3",
			"test|view1|col_2",
			"test|view1|max4",
			"test|view1|min(col_1)"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns
				where TABLE_NAME = 'view1' or TABLE_NAME = 'tbl1'`).Check(
		testkit.RowsWithSep("|",
			"test|tbl1|col_1",
			"test|tbl1|col_2",
			"test|tbl1|col_4",
			"test|view1|min(col_1)",
			"test|view1|col_2",
			"test|view1|max4"))
	tk.MustQuery("select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns where COLUMN_NAME = \"col_2\";").Sort().Check(
		testkit.RowsWithSep("|",
			"test|tbl1|col_2",
			"test|tbl2|col_2",
			"test|view1|col_2"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns
				where TABLE_SCHEMA = 'test' and TABLE_NAME = 'tbl2';`).Check(
		testkit.RowsWithSep("|",
			"test|tbl2|col_1",
			"test|tbl2|col_2",
			"test|tbl2|col_3"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns
				where TABLE_SCHEMA = 'test' and COLUMN_NAME = 'col_4'`).Check(
		testkit.RowsWithSep("|",
			"test|tbl1|col_4"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns
				where TABLE_NAME = 'view1' and COLUMN_NAME like 'm%%';`).Check(
		testkit.RowsWithSep("|",
			"test|view1|min(col_1)",
			"test|view1|max4"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME from information_schema.columns
				where TABLE_SCHEMA = 'test' and TABLE_NAME = 'tbl1' and COLUMN_NAME = 'col_2';`).Check(
		testkit.RowsWithSep("|",
			"test|tbl1|col_2"))
	tk.MustQuery(`select count(*) from information_schema.columns
				where TABLE_SCHEMA = 'test' and TABLE_NAME in ('tbl1', 'tbl2', 'view1');`).Check(
		testkit.RowsWithSep("|", "9"))
}

func TestIndexUsageTable(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table idt1(col_1 int primary key, col_2 int, index idx_1(col_1), index idx_2(col_2), index idx_3(col_1, col_2));")
	tk.MustExec("create table idt2(col_1 int primary key, col_2 int, index idx_1(col_1), index idx_2(col_2), index idx_4(col_2, col_1));")
	tk.MustExec("create table idt3(col_1 varchar(255) primary key);")
	tk.MustExec("create table idt4(col_1 varchar(255) primary key NONCLUSTERED);")
	tk.MustExec("create table idt5(col_1 int);")

	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test';`).Sort().Check(
		testkit.RowsWithSep("|",
			"test|idt1|idx_1",
			"test|idt1|idx_2",
			"test|idt1|idx_3",
			"test|idt1|primary",
			"test|idt2|idx_1",
			"test|idt2|idx_2",
			"test|idt2|idx_4",
			"test|idt2|primary",
			"test|idt3|primary",
			"test|idt4|primary"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage where TABLE_NAME = 'idt1'`).Sort().Check(
		testkit.RowsWithSep("|",
			"test|idt1|idx_1",
			"test|idt1|idx_2",
			"test|idt1|idx_3",
			"test|idt1|primary"))
	tk.MustQuery("select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage where INDEX_NAME = 'IDX_3'").Check(
		testkit.RowsWithSep("|",
			"test|idt1|idx_3"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test' and TABLE_NAME = 'idt1';`).Sort().Check(
		testkit.RowsWithSep("|",
			"test|idt1|idx_1",
			"test|idt1|idx_2",
			"test|idt1|idx_3",
			"test|idt1|primary"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test' and INDEX_NAME = 'idx_2';`).Sort().Check(
		testkit.RowsWithSep("|",
			"test|idt1|idx_2",
			"test|idt2|idx_2"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_NAME = 'idt1' and INDEX_NAME = 'idx_1';`).Check(
		testkit.RowsWithSep("|",
			"test|idt1|idx_1"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test' and TABLE_NAME = 'idt2' and INDEX_NAME = 'idx_4';`).Check(
		testkit.RowsWithSep("|",
			"test|idt2|idx_4"))
	tk.MustQuery(`select count(*) from information_schema.tidb_index_usage
	where TABLE_SCHEMA = 'test' and TABLE_NAME in ('idt1', 'idt2');`).Check(
		testkit.RowsWithSep("|", "8"))

	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test1';`).Check(testkit.Rows())
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_NAME = 'idt3';`).Check(testkit.Rows("test idt3 primary"))
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where INDEX_NAME = 'IDX_5';`).Check(testkit.Rows())
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test' and TABLE_NAME = 'idt0';`).Check(testkit.Rows())
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test1' and INDEX_NAME = 'idx_2';`).Check(testkit.Rows())
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_NAME = 'idt2' and INDEX_NAME = 'idx_3';`).Check(testkit.Rows())
	tk.MustQuery(`select TABLE_SCHEMA, TABLE_NAME, INDEX_NAME from information_schema.tidb_index_usage
				where TABLE_SCHEMA = 'test' and TABLE_NAME = 'idt1' and INDEX_NAME = 'idx_4';`).Check(testkit.Rows())
}

// https://github.com/pingcap/tidb/issues/32459.
func TestJoinSystemTableContainsView(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t (a timestamp, b int);")
	tk.MustExec("insert into t values (null, 100);")
	tk.MustExec("create view v as select * from t;")
	// This is used by grafana when TiDB is specified as the data source.
	// See https://github.com/grafana/grafana/blob/e86b6662a187c77656f72bef3b0022bf5ced8b98/public/app/plugins/datasource/mysql/meta_query.ts#L31
	for range 10 {
		tk.MustQueryWithContext(context.Background(), `
SELECT
    table_name as table_name,
    ( SELECT
        column_name as column_name
      FROM information_schema.columns c
      WHERE
        c.table_schema = t.table_schema AND
        c.table_name = t.table_name AND
        c.data_type IN ('timestamp', 'datetime')
      ORDER BY ordinal_position LIMIT 1
    ) AS time_column,
    ( SELECT
        column_name AS column_name
      FROM information_schema.columns c
      WHERE
        c.table_schema = t.table_schema AND
        c.table_name = t.table_name AND
        c.data_type IN('float', 'int', 'bigint')
      ORDER BY ordinal_position LIMIT 1
    ) AS value_column
  FROM information_schema.tables t
  WHERE
    t.table_schema = database() AND
    EXISTS
    ( SELECT 1
      FROM information_schema.columns c
      WHERE
        c.table_schema = t.table_schema AND
        c.table_name = t.table_name AND
        c.data_type IN ('timestamp', 'datetime')
    ) AND
    EXISTS
    ( SELECT 1
      FROM information_schema.columns c
      WHERE
        c.table_schema = t.table_schema AND
        c.table_name = t.table_name AND
        c.data_type IN('float', 'int', 'bigint')
    )
  LIMIT 1
;
`)
	}
}

// https://github.com/pingcap/tidb/issues/36426.
func TestShowColumnsWithSubQueryView(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")

	tk.MustExec("set @@global.tidb_schema_cache_size = 0;")
	t.Cleanup(func() {
		tk.MustExec("set @@global.tidb_schema_cache_size = default;")
	})

	tk.MustExec("CREATE TABLE added (`id` int(11), `name` text, `some_date` timestamp);")
	tk.MustExec("CREATE TABLE incremental (`id` int(11), `name`text, `some_date` timestamp);")
	tk.MustExec("create view temp_view as (select * from `added` where id > (select max(id) from `incremental`));")
	// Show columns should not send coprocessor request to the storage.
	testfailpoint.Enable(t, "tikvclient/tikvStoreSendReqResult", `return("timeout")`)
	testfailpoint.Enable(t, "github.com/pingcap/tidb/pkg/planner/core/BuildDataSourceFailed", "panic")

	tk.MustQuery("show columns from temp_view;").Check(testkit.Rows(
		"id int(11) YES  <nil> ",
		"name text YES  <nil> ",
		"some_date timestamp YES  <nil> "))
	tk.MustQuery("select COLUMN_NAME from information_schema.columns where table_name = 'temp_view';").Check(testkit.Rows("id", "name", "some_date"))
}

// Code below are helper utilities for the test cases.

type getTiFlashSystemTableRequestMocker struct {
	tikv.Client
	t        *testing.T
	handlers map[string]func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error)
}

func (client *getTiFlashSystemTableRequestMocker) SendRequest(ctx context.Context, addr string, req *tikvrpc.Request, timeout time.Duration) (*tikvrpc.Response, error) {
	if req.Type == tikvrpc.CmdGetTiFlashSystemTable {
		if handler, ok := client.handlers[req.Req.(*kvrpcpb.TiFlashSystemTableRequest).Sql]; ok {
			resp, err := handler(req.GetTiFlashSystemTable())
			if err != nil {
				return nil, err
			}
			return &tikvrpc.Response{Resp: resp}, nil
		}
		// If we enter here, it means no handler is matching. We should fail!
		require.Fail(client.t, fmt.Sprintf("Received request %s but no matching handler, maybe caused by unexpected query", req.Req.(*kvrpcpb.TiFlashSystemTableRequest).Sql))
	}
	return client.Client.SendRequest(ctx, addr, req, timeout)
}

func (client *getTiFlashSystemTableRequestMocker) MockQuery(query string, fn func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error)) *getTiFlashSystemTableRequestMocker {
	client.handlers[query] = fn
	return client
}

func (client *getTiFlashSystemTableRequestMocker) AsOpt() mockstore.MockTiKVStoreOption {
	return mockstore.WithClientHijacker(func(kvClient tikv.Client) tikv.Client {
		client.Client = kvClient
		return client
	})
}

func newGetTiFlashSystemTableRequestMocker(t *testing.T) *getTiFlashSystemTableRequestMocker {
	return &getTiFlashSystemTableRequestMocker{
		handlers: make(map[string]func(req *kvrpcpb.TiFlashSystemTableRequest) (*kvrpcpb.TiFlashSystemTableResponse, error), 0),
		t:        t,
	}
}

// https://github.com/pingcap/tidb/issues/52350
func TestReferencedTableSchemaWithForeignKey(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("create database if not exists test;")
	tk.MustExec("create database if not exists test2;")
	tk.MustExec("drop table if exists test.t1;")
	tk.MustExec("drop table if exists test2.t2;")
	tk.MustExec("create table test.t1(id int primary key);")
	tk.MustExec("create table test2.t2(i int, id int, foreign key (id) references test.t1(id));")

	tk.MustQuery(`SELECT column_name, referenced_column_name, referenced_table_name, table_schema, referenced_table_schema
	FROM information_schema.key_column_usage
	WHERE table_name = 't2' AND table_schema = 'test2';`).Check(testkit.Rows(
		"id id t1 test2 test"))
}

func TestSameTableNameInTwoSchemas(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("create database test1;")
	tk.MustExec("create database test2;")
	tk.MustExec("create table test1.t (a int);")
	tk.MustExec("create table test2.t (a int);")

	rs := tk.MustQuery("select tidb_table_id from information_schema.tables where table_name = 't' and table_schema = 'test1';").Rows()
	t1ID, err := strconv.Atoi(rs[0][0].(string))
	require.NoError(t, err)
	rs = tk.MustQuery("select tidb_table_id from information_schema.tables where table_name = 't' and table_schema = 'test2';").Rows()
	t2ID, err := strconv.Atoi(rs[0][0].(string))
	require.NoError(t, err)

	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where tidb_table_id = %d;", t1ID)).
		Check(testkit.Rows(fmt.Sprintf("test1 t %d", t1ID)))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where tidb_table_id = %d;", t2ID)).
		Check(testkit.Rows(fmt.Sprintf("test2 t %d", t2ID)))

	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_name = 't' and tidb_table_id = %d;", t1ID)).
		Check(testkit.Rows(fmt.Sprintf("test1 t %d", t1ID)))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'test1' and tidb_table_id = %d;", t1ID)).
		Check(testkit.Rows(fmt.Sprintf("test1 t %d", t1ID)))
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_name = 'unknown' and tidb_table_id = %d;", t1ID)).
		Check(testkit.Rows())
	tk.MustQuery(fmt.Sprintf("select table_schema, table_name, tidb_table_id from information_schema.tables where table_schema = 'unknown' and tidb_table_id = %d;", t1ID)).
		Check(testkit.Rows())
}

func TestInfoSchemaDDLJobs(t *testing.T) {
	store := testkit.CreateMockStore(t)

	tk := testkit.NewTestKit(t, store)
	for i := range 2 {
		tk.MustExec(fmt.Sprintf("create database d%d", i))
		tk.MustExec(fmt.Sprintf("use d%d", i))
		for j := range 4 {
			tk.MustExec(fmt.Sprintf("create table t%d(id int, col1 int, col2 int)", j))
			tk.MustExec(fmt.Sprintf("alter table t%d add index (col1)", j))
		}
	}

	tk2 := testkit.NewTestKit(t, store)
	tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE table_name = "t1";`).Check(testkit.RowsWithSep("|",
		"135|add index|public|128|133|t1|synced",
		"134|create table|public|128|133|t1|synced",
		"121|add index|public|114|119|t1|synced",
		"120|create table|public|114|119|t1|synced",
	))
	tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE db_name = "d1" and JOB_TYPE LIKE "add index%%";`).Check(testkit.RowsWithSep("|",
		"141|add index|public|128|139|t3|synced",
		"138|add index|public|128|136|t2|synced",
		"135|add index|public|128|133|t1|synced",
		"132|add index|public|128|130|t0|synced",
	))
	tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE db_name = "d0" and table_name = "t3";`).Check(testkit.RowsWithSep("|",
		"127|add index|public|114|125|t3|synced",
		"126|create table|public|114|125|t3|synced",
	))
	tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
					FROM information_schema.ddl_jobs WHERE state = "running";`).Check(testkit.Rows())

	// Test running job
	loaded := atomic.Bool{}
	testfailpoint.EnableCall(t, "github.com/pingcap/tidb/pkg/ddl/beforeRunOneJobStep", func(job *model.Job) {
		if job.SchemaState == model.StateWriteOnly && loaded.CompareAndSwap(false, true) {
			tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE table_name = "t0" and state = "running";`).Check(testkit.RowsWithSep("|",
				"142 add index write only 114 116 t0 running",
			))
			tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE db_name = "d0" and state = "running";`).Check(testkit.RowsWithSep("|",
				"142 add index write only 114 116 t0 running",
			))
			tk2.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE state = "running";`).Check(testkit.RowsWithSep("|",
				"142 add index write only 114 116 t0 running",
			))
		}
	})

	tk.MustExec("use d0")
	tk.MustExec("alter table t0 add index (col2)")

	// Test search history jobs
	tk.MustExec("create database test2")
	tk.MustExec("create table test2.t1(id int)")
	tk.MustExec("drop database test2")
	tk.MustExec("create database test2")
	tk.MustExec("create table test2.t1(id int)")
	tk.MustQuery(`SELECT JOB_ID, JOB_TYPE, SCHEMA_STATE, SCHEMA_ID, TABLE_ID, table_name, STATE
				   FROM information_schema.ddl_jobs WHERE db_name = "test2" and table_name = "t1"`).Check(testkit.RowsWithSep("|",
		"151|create table|public|148|150|t1|synced",
		"146|create table|public|143|145|t1|synced",
	))

	// Test explain output, since the output may change in future.
	tk.MustQuery(`EXPLAIN FORMAT='brief' SELECT * FROM information_schema.ddl_jobs where db_name = "test2" limit 10;`).Check(testkit.Rows(
		`Limit 10.00 root  offset:0, count:10`,
		`└─Selection 10.00 root  eq(Column#2, "test2")`,
		`  └─MemTableScan 10000.00 root table:DDL_JOBS db_name:["test2"]`,
	))
}

func TestInfoSchemaConditionWorks(t *testing.T) {
	// this test creates table in different schema with different index name, and check
	// the condition in the following columns whether work as expected.
	//
	// - "table_schema"
	// - "constraint_schema"
	// - "table_name"
	// - "constraint_name"
	// - "partition_name"
	// - "schema_name"
	// - "index_name"
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	for db := range 2 {
		for table := range 2 {
			tk.MustExec(fmt.Sprintf("create database if not exists Db%d;", db))
			tk.MustExec(fmt.Sprintf(`create table Db%d.Table%d (id int primary key, data0 varchar(255), data1 varchar(255))
				partition by range (id) (
					partition p0 values less than (10),
					partition p1 values less than (20)
				);`, db, table))
			for index := range 2 {
				tk.MustExec(fmt.Sprintf("create unique index Idx%d on Db%d.Table%d (id, data%d);", index, db, table, index))
			}
		}
	}

	testColumns := map[string]string{
		"table_schema":      "db",
		"constraint_schema": "db",
		"table_name":        "table",
		"constraint_name":   "idx",
		"partition_name":    "p",
		"schema_name":       "db",
		"index_name":        "idx",
	}
	testTables := []string{}
	for _, row := range tk.MustQuery("show tables in information_schema").Rows() {
		tableName := row[0].(string)
		// exclude some tables which cannot run without TiKV.
		if strings.HasPrefix(tableName, "CLUSTER_") ||
			strings.HasPrefix(tableName, "INSPECTION_") ||
			strings.HasPrefix(tableName, "METRICS_") ||
			strings.HasPrefix(tableName, "TIFLASH_") ||
			strings.HasPrefix(tableName, "TIKV_") ||
			strings.HasPrefix(tableName, "USER_") ||
			tableName == "TABLE_STORAGE_STATS" ||
			strings.Contains(tableName, "REGION") {
			continue
		}
		testTables = append(testTables, row[0].(string))
	}
	for _, table := range testTables {
		rs, err := tk.Exec(fmt.Sprintf("select * from information_schema.%s", table))
		require.NoError(t, err)
		cols := rs.Fields()

		chk := rs.NewChunk(nil)
		rowCount := 0
		for {
			err := rs.Next(context.Background(), chk)
			require.NoError(t, err)
			if chk.NumRows() == 0 {
				break
			}
			rowCount += chk.NumRows()
		}
		if rowCount == 0 {
			// TODO: find a way to test the table without any rows by adding some rows to them.
			continue
		}
		for i := range cols {
			colName := cols[i].Column.Name.L
			if valPrefix, ok := testColumns[colName]; ok {
				for j := range 2 {
					sql := fmt.Sprintf("select * from information_schema.%s where %s = '%s%d';",
						table, colName, valPrefix, j)
					rows := tk.MustQuery(sql).Rows()
					rowCountWithCondition := len(rows)
					require.Less(t, rowCountWithCondition, rowCount, "%s has no effect on %s. SQL: %s", colName, table, sql)

					// check the condition works as expected
					for _, row := range rows {
						require.Equal(t, fmt.Sprintf("%s%d", valPrefix, j), strings.ToLower(row[i].(string)),
							"%s has no effect on %s. SQL: %s", colName, table, sql)
					}
				}
			}
		}
	}

	// Test the PRIMARY constraint filter
	rows := tk.MustQuery("select constraint_name, table_schema from information_schema.table_constraints where constraint_name = 'PRIMARY' and table_schema = 'db0';").Rows()
	require.Equal(t, 2, len(rows))
	for _, row := range rows {
		require.Equal(t, "PRIMARY", row[0].(string))
		require.Equal(t, "Db0", row[1].(string))
	}
	rows = tk.MustQuery("select constraint_name, table_schema from information_schema.key_column_usage where constraint_name = 'PRIMARY' and table_schema = 'db1';").Rows()
	require.Equal(t, 2, len(rows))
	for _, row := range rows {
		require.Equal(t, "PRIMARY", row[0].(string))
		require.Equal(t, "Db1", row[1].(string))
	}

	// Test the `partition_name` filter
	tk.MustExec("create database if not exists db_no_partition;")
	tk.MustExec("create table db_no_partition.t_no_partition (id int primary key, data0 varchar(255), data1 varchar(255));")
	tk.MustExec(`create table db_no_partition.t_partition (id int primary key, data0 varchar(255), data1 varchar(255))
		partition by range (id) (
			partition p0 values less than (10),
			partition p1 values less than (20)
		);`)
	rows = tk.MustQuery("select * from information_schema.partitions where table_schema = 'db_no_partition' and partition_name is NULL;").Rows()
	require.Equal(t, 1, len(rows))
	rows = tk.MustQuery("select * from information_schema.partitions where table_schema = 'db_no_partition' and (partition_name is NULL or partition_name = 'p0');").Rows()
	require.Equal(t, 2, len(rows))
}

func TestInfoschemaTablesSpecialOptimizationCovered(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("set @@global.tidb_schema_cache_size = default")

	for _, testCase := range []struct {
		sql    string
		expect bool
	}{
		{"select table_name, table_schema from information_schema.tables", true},
		{"select table_name from information_schema.tables", true},
		{"select table_name from information_schema.tables where table_schema = 'test'", true},
		{"select table_name, table_schema from information_schema.tables where table_name = 't'", true},
		{"select table_schema from information_schema.tables", true},
		{"select table_schema from information_schema.tables where tidb_table_id = 4611686018427387967", false},
		{"select count(table_schema) from information_schema.tables", true},
		{"select count(table_name) from information_schema.tables", true},
		{"select count(table_rows) from information_schema.tables", false},
		{"select count(1) from information_schema.tables", true},
		{"select count(*) from information_schema.tables", true},
		{"select count(*) from information_schema.tables where tidb_table_id = 4611686018427387967", false},
		{"select count(1) from (select table_name from information_schema.tables) t", true},
		{"select * from information_schema.tables", false},
		{"select table_name, table_catalog from information_schema.tables", true},
		{"select table_name, table_catalog from information_schema.tables where table_catalog = 'normal'", true},
		{"select table_name, table_rows from information_schema.tables", false},
		{"select table_name, table_schema, tidb_table_id from information_schema.tables where tidb_table_id = 4611686018427387967", false},
	} {
		var covered bool
		ctx := context.WithValue(context.Background(), "cover-check", &covered)
		tk.MustQueryWithContext(ctx, testCase.sql)
		require.Equal(t, testCase.expect, covered, testCase.sql)
	}
}

func TestIndexUsageWithData(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	// Some bad tests will set the global variable to 0, and they don't set it back. So even if the default value for this variable is 1,
	// we'll need to set it to 1 here.
	tk.MustExec("set global tidb_enable_collect_execution_info=1;")
	tk.RefreshSession()

	insertDataAndScanToT := func(indexName string) {
		// insert 1000 rows
		tk.MustExec("INSERT into t WITH RECURSIVE cte AS (select 1 as n UNION ALL select n+1 FROM cte WHERE n < 1000) select n from cte;")
		tk.MustExec("ANALYZE TABLE t")

		// full scan
		sql := fmt.Sprintf("SELECT * FROM t use index(%s) ORDER BY a", indexName)
		rows := tk.MustQuery(sql).Rows()
		require.Len(t, rows, 1000)
		for i, r := range rows {
			require.Equal(t, r[0], strconv.Itoa(i+1))
		}

		logutil.BgLogger().Info("execute with plan",
			zap.String("sql", sql),
			zap.String("plan", tk.MustQuery("explain "+sql).String()))

		// scan 1/4 of the rows
		sql = fmt.Sprintf("SELECT * FROM t use index(%s) WHERE a <= 250 ORDER BY a", indexName)
		rows = tk.MustQuery(sql).Rows()
		require.Len(t, rows, 250)
		for i, r := range rows {
			require.Equal(t, r[0], strconv.Itoa(i+1))
		}

		logutil.BgLogger().Info("execute with plan",
			zap.String("sql", sql),
			zap.String("plan", tk.MustQuery("explain "+sql).String()))
	}

	checkIndexUsage := func(startQuery time.Time, endQuery time.Time) {
		require.Eventually(t, func() bool {
			rows := tk.MustQuery("select QUERY_TOTAL,PERCENTAGE_ACCESS_20_50,PERCENTAGE_ACCESS_100,LAST_ACCESS_TIME from information_schema.tidb_index_usage where table_schema = 'test'").Rows()

			if len(rows) != 1 {
				return false
			}
			if rows[0][0] != "2" || rows[0][1] != "1" || rows[0][2] != "1" {
				return false
			}
			lastAccessTime, err := time.ParseInLocation(time.DateTime, rows[0][3].(string), time.Local)
			if err != nil {
				return false
			}
			if lastAccessTime.Unix() < startQuery.Unix() || lastAccessTime.Unix() > endQuery.Unix() {
				return false
			}

			return true
		}, 5*time.Second, 100*time.Millisecond)
	}

	t.Run("test index usage with normal index", func(t *testing.T) {
		tk.MustExec("use test")
		tk.MustExec("create table t (a int, index idx(a));")
		defer tk.MustExec("drop table t")

		tk.MustQuery("select * from information_schema.tidb_index_usage where table_schema = 'test'").Check(testkit.Rows(
			"test t idx 0 0 0 0 0 0 0 0 0 0 <nil>",
		))

		startQuery := time.Now()
		insertDataAndScanToT("idx")
		endQuery := time.Now()

		checkIndexUsage(startQuery, endQuery)
	})

	t.Run("test index usage with integer primary key", func(t *testing.T) {
		tk.MustExec("use test")
		tk.MustExec("create table t (a int primary key);")
		defer tk.MustExec("drop table t")

		tk.MustQuery("select * from information_schema.tidb_index_usage where table_schema = 'test'").Check(testkit.Rows(
			"test t primary 0 0 0 0 0 0 0 0 0 0 <nil>",
		))

		startQuery := time.Now()
		insertDataAndScanToT("primary")
		endQuery := time.Now()

		checkIndexUsage(startQuery, endQuery)
	})

	t.Run("test index usage with integer clustered primary key", func(t *testing.T) {
		tk.MustExec("use test")
		tk.MustExec("create table t (a bigint primary key clustered);")
		defer tk.MustExec("drop table t")

		tk.MustQuery("select * from information_schema.tidb_index_usage where table_schema = 'test'").Check(testkit.Rows(
			"test t primary 0 0 0 0 0 0 0 0 0 0 <nil>",
		))

		startQuery := time.Now()
		insertDataAndScanToT("primary")
		endQuery := time.Now()

		checkIndexUsage(startQuery, endQuery)
	})

	t.Run("test index usage with string primary key", func(t *testing.T) {
		tk.MustExec("use test")
		tk.MustExec("create table t (a varchar(16) primary key clustered);")
		defer tk.MustExec("drop table t")

		tk.MustQuery("select * from information_schema.tidb_index_usage where table_schema = 'test'").Check(testkit.Rows(
			"test t primary 0 0 0 0 0 0 0 0 0 0 <nil>",
		))

		tk.MustExec("INSERT into t WITH RECURSIVE cte AS (select 1 as n UNION ALL select n+1 FROM cte WHERE n < 1000) select n from cte;")
		tk.MustExec("ANALYZE TABLE t")

		// full scan
		rows := tk.MustQuery("SELECT * FROM t ORDER BY a").Rows()
		require.Len(t, rows, 1000)

		// scan 1/4 of the rows
		startQuery := time.Now()
		rows = tk.MustQuery("select * from t where a < '3'").Rows()
		require.Len(t, rows, 223)
		endQuery := time.Now()

		checkIndexUsage(startQuery, endQuery)
	})

	t.Run("test index usage with nonclustered primary key", func(t *testing.T) {
		tk.MustExec("use test")
		tk.MustExec("create table t (a int primary key nonclustered);")
		defer tk.MustExec("drop table t")

		tk.MustQuery("select * from information_schema.tidb_index_usage where table_schema = 'test'").Check(testkit.Rows(
			"test t primary 0 0 0 0 0 0 0 0 0 0 <nil>",
		))

		startQuery := time.Now()
		insertDataAndScanToT("primary")
		endQuery := time.Now()

		checkIndexUsage(startQuery, endQuery)
	})
}

func TestKeyspaceMeta(t *testing.T) {
	keyspaceID := rand.Uint32() >> 8
	keyspaceName := fmt.Sprintf("keyspace-%d", keyspaceID)
	cfg := map[string]string{
		"key_a": "a",
		"key_b": "b",
	}

	keyspaceMeta := &keyspacepb.KeyspaceMeta{
		Id:     keyspaceID,
		Name:   keyspaceName,
		Config: cfg,
	}

	store := testkit.CreateMockStore(t, mockstore.WithKeyspaceMeta(keyspaceMeta))
	tk := testkit.NewTestKit(t, store)

	rows := tk.MustQuery("select * from information_schema.keyspace_meta").Rows()
	require.Equal(t, 1, len(rows))
	require.Equal(t, keyspaceMeta.Name, rows[0][0])
	require.Equal(t, fmt.Sprintf("%d", keyspaceMeta.Id), rows[0][1])
	actualCfg := make(map[string]string)
	err := json.Unmarshal([]byte(rows[0][2].(string)), &actualCfg)
	require.Nil(t, err)
	require.Equal(t, cfg, actualCfg)
}

func TestStatisticShowPublicIndexes(t *testing.T) {
	store := testkit.CreateMockStore(t)
	tk := testkit.NewTestKit(t, store)
	tk.MustExec("use test")
	tk.MustExec("create table t (a int, b int);")
	tk.MustExec("insert into t values (1, 1);")

	tk1 := testkit.NewTestKit(t, store)
	tk1.MustExec("use test")
	testfailpoint.EnableCall(t, "github.com/pingcap/tidb/pkg/ddl/afterWaitSchemaSynced", func(job *model.Job) {
		if job.Type != model.ActionAddIndex || job.SchemaState == model.StatePublic {
			return
		}
		rs := tk1.MustQuery(`SELECT count(1) FROM INFORMATION_SCHEMA.STATISTICS where
			TABLE_SCHEMA = 'test' and table_name = 't' and index_name = 'idx';`).Rows()
		require.Equal(t, "0", rs[0][0].(string))
	})
	tk.MustExec("alter table t add index idx(b);")
}
