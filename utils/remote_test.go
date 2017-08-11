package utils_test

import (
	"fmt"
	"os"
	"os/user"

	"github.com/greenplum-db/gpbackup/testutils"
	"github.com/greenplum-db/gpbackup/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

var _ = Describe("utils/io tests", func() {
	var connection *utils.DBConn
	var mock sqlmock.Sqlmock

	masterSeg := utils.SegConfig{-1, "localhost", "/data/gpseg-1"}
	localSegOne := utils.SegConfig{0, "localhost", "/data/gpseg0"}
	remoteSegOne := utils.SegConfig{1, "remotehost1", "/data/gpseg1"}
	remoteSegTwo := utils.SegConfig{2, "remotehost2", "/data/gpseg2"}

	BeforeEach(func() {
		connection, mock = testutils.CreateAndConnectMockDB()
		testutils.SetupTestLogger()
		utils.System.CurrentUser = func() (*user.User, error) { return &user.User{Username: "testUser", HomeDir: "testDir"}, nil }
		utils.System.Hostname = func() (string, error) { return "testHost", nil }

	})
	Describe("ExecuteClusterCommand", func() {
		BeforeEach(func() {
			os.MkdirAll("/tmp/backup_and_restore_test", 0777)
		})
		AfterEach(func() {
			os.RemoveAll("/tmp/backup_and_restore_test")
		})
		It("runs commands specified by command map", func() {
			cluster := utils.Cluster{}
			commandMap := map[int][]string{
				-1: {"touch", "/tmp/backup_and_restore_test/foo"},
				0:  {"touch", "/tmp/backup_and_restore_test/baz"},
			}
			cluster.ExecuteClusterCommand(commandMap)

			testutils.ExpectPathToExist("/tmp/backup_and_restore_test/foo")
			testutils.ExpectPathToExist("/tmp/backup_and_restore_test/baz")
		})
		It("returns any errors generated by any of the commands", func() {
			cluster := utils.Cluster{}
			commandMap := map[int][]string{
				-1: {"touch", "/tmp/backup_and_restore_test/foo"},
				0:  {"some-non-existant-command"},
			}
			errMap := cluster.ExecuteClusterCommand(commandMap)

			testutils.ExpectPathToExist("/tmp/backup_and_restore_test/foo")
			Expect(len(errMap)).To(Equal(1))
			Expect(errMap[0].Error()).To(Equal("exec: \"some-non-existant-command\": executable file not found in $PATH"))
		})
	})
	Describe("ConstructSSHCommand", func() {
		It("constructs an ssh command", func() {
			cmd := utils.ConstructSSHCommand("some-host", "ls")
			Expect(cmd).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@some-host", "ls"}))
		})
	})
	Describe("GenerateCommandMap", func() {
		It("Returns a map of ssh commands for a single segment", func() {
			cluster := utils.NewCluster([]utils.SegConfig{remoteSegOne}, "", "20170101010101")
			commandMap := cluster.GenerateSSHCommandMap(func(_ int) string {
				return "ls"
			})
			Expect(len(commandMap)).To(Equal(1))
			Expect(commandMap[1]).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@remotehost1", "ls"}))
		})
		It("Returns a map of ssh commands for two segments on the same host", func() {
			cluster := utils.NewCluster([]utils.SegConfig{masterSeg, localSegOne}, "", "20170101010101")
			commandMap := cluster.GenerateSSHCommandMap(func(_ int) string {
				return "ls"
			})
			Expect(len(commandMap)).To(Equal(2))
			Expect(commandMap[-1]).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@localhost", "ls"}))
			Expect(commandMap[0]).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@localhost", "ls"}))
		})
		It("Returns a map of ssh commands for three segments on different hosts", func() {
			cluster := utils.NewCluster([]utils.SegConfig{localSegOne, remoteSegOne, remoteSegTwo}, "", "20170101010101")
			commandMap := cluster.GenerateSSHCommandMap(func(contentID int) string {
				return fmt.Sprintf("mkdir -p %s", cluster.GetDirForContent(contentID))
			})
			Expect(len(commandMap)).To(Equal(3))
			Expect(commandMap[0]).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@localhost", "mkdir -p /data/gpseg0/backups/20170101/20170101010101"}))
			Expect(commandMap[1]).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@remotehost1", "mkdir -p /data/gpseg1/backups/20170101/20170101010101"}))
			Expect(commandMap[2]).To(Equal([]string{"ssh", "-o", "StrictHostKeyChecking=no", "testUser@remotehost2", "mkdir -p /data/gpseg2/backups/20170101/20170101010101"}))
		})
	})
	Describe("Cluster", func() {
		masterSeg := utils.SegConfig{-1, "localhost", "/data/gpseg-1"}
		localSegOne := utils.SegConfig{0, "localhost", "/data/gpseg0"}
		localSegTwo := utils.SegConfig{1, "localhost", "/data/gpseg1"}
		remoteSegTwo := utils.SegConfig{1, "remotehost", "/data/gpseg1"}
		Context("when base dir is not overridden", func() {
			It("generates table file path for copy command", func() {
				cluster := utils.NewCluster(nil, "", "20170101010101")
				Expect(cluster.GetTableBackupFilePathForCopyCommand(1234)).To(Equal("<SEG_DATA_DIR>/backups/20170101/20170101010101/gpbackup_<SEGID>_20170101010101_1234"))
			})

			It("sets up the configuration for a single-host, single-segment cluster", func() {
				cluster := utils.NewCluster([]utils.SegConfig{masterSeg, localSegOne}, "", "20170101010101")
				Expect(len(cluster.GetContentList())).To(Equal(2))
				Expect(cluster.GetDirForContent(-1)).To(Equal("/data/gpseg-1/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(-1)).To(Equal("localhost"))
				Expect(cluster.GetDirForContent(0)).To(Equal("/data/gpseg0/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(0)).To(Equal("localhost"))
			})
			It("sets up the configuration for a single-host, multi-segment cluster", func() {
				cluster := utils.NewCluster([]utils.SegConfig{masterSeg, localSegOne, localSegTwo}, "", "20170101010101")
				Expect(len(cluster.GetContentList())).To(Equal(3))
				Expect(cluster.GetDirForContent(-1)).To(Equal("/data/gpseg-1/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(-1)).To(Equal("localhost"))
				Expect(cluster.GetDirForContent(0)).To(Equal("/data/gpseg0/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(0)).To(Equal("localhost"))
				Expect(cluster.GetDirForContent(1)).To(Equal("/data/gpseg1/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(1)).To(Equal("localhost"))
			})
			It("sets up the configuration for a multi-host, multi-segment cluster", func() {
				cluster := utils.NewCluster([]utils.SegConfig{masterSeg, localSegOne, remoteSegTwo}, "", "20170101010101")
				Expect(len(cluster.GetContentList())).To(Equal(3))
				Expect(cluster.GetDirForContent(-1)).To(Equal("/data/gpseg-1/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(-1)).To(Equal("localhost"))
				Expect(cluster.GetDirForContent(0)).To(Equal("/data/gpseg0/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(0)).To(Equal("localhost"))
				Expect(cluster.GetDirForContent(1)).To(Equal("/data/gpseg1/backups/20170101/20170101010101"))
				Expect(cluster.GetHostForContent(1)).To(Equal("remotehost"))
			})
			It("GetTableMapFilePath() uses segment configuration for backup directory", func() {
				cluster := utils.NewCluster([]utils.SegConfig{masterSeg}, "", "20170101010101")
				Expect(cluster.GetTableMapFilePath()).To(Equal("/data/gpseg-1/backups/20170101/20170101010101/gpbackup_20170101010101_table_map"))
			})
		})
		Context("when base dir is overridden", func() {
			It("GetDirForContent() uses user specified path for the content directory", func() {
				cluster := utils.NewCluster([]utils.SegConfig{masterSeg}, "/foo/bar", "20170101010101")
				Expect(cluster.GetDirForContent(-1)).To(Equal("/foo/bar/gpseg-1/backups/20170101/20170101010101"))
			})
			It("GetTableBackupFilePathForCopyCommand() uses user specified path for table backup file", func() {
				cluster := utils.NewCluster(nil, "/foo/bar", "20170101010101")
				Expect(cluster.GetTableBackupFilePathForCopyCommand(1234)).To(Equal("/foo/bar/gpseg<SEGID>/backups/20170101/20170101010101/gpbackup_<SEGID>_20170101010101_1234"))
			})
			It("GetTableMapFilePath() uses user specified path for table backup file", func() {
				cluster := utils.NewCluster(nil, "/foo/bar", "20170101010101")
				Expect(cluster.GetTableMapFilePath()).To(Equal("/foo/bar/gpseg-1/backups/20170101/20170101010101/gpbackup_20170101010101_table_map"))
			})
		})
	})
})
