package restore

import (
	"flag"
	"fmt"
	"os"

	"github.com/greenplum-db/gpbackup/utils"

	"github.com/pkg/errors"
)

var (
	connection    *utils.DBConn
	logger        *utils.Logger
	globalCluster utils.Cluster
	backupReport  utils.Report
	version       string
)

var ( // Command-line flags
	debug          *bool
	dumpDir        *string
	quiet          *bool
	timestamp      *string
	verbose        *bool
	restoreGlobals *bool
	printVersion   *bool
)

// We define and initialize flags separately to avoid import conflicts in tests
func initializeFlags() {
	debug = flag.Bool("debug", false, "Print verbose and debug log messages")
	dumpDir = flag.String("dumpdir", "", "The directory in which the dump files to be restored are located")
	quiet = flag.Bool("quiet", false, "Suppress non-warning, non-error log messages")
	timestamp = flag.String("timestamp", "", "The timestamp to be restored, in the format YYYYMMDDHHMMSS")
	verbose = flag.Bool("verbose", false, "Print verbose log messages")
	restoreGlobals = flag.Bool("globals", false, "Restore global metadata")
	printVersion = flag.Bool("version", false, "Print version number and exit")
}

// This function handles setup that can be done before parsing flags.
func DoInit() {
	SetLogger(utils.InitializeLogging("gprestore", ""))
}

func SetLogger(log *utils.Logger) {
	logger = log
}

/*
* This function handles argument parsing and validation, e.g. checking that a passed filename exists.
* It should only validate; initialization with any sort of side effects should go in DoInit or DoSetup.
 */
func DoValidation() {
	initializeFlags()
	flag.Parse()
	if *printVersion {
		fmt.Printf("gprestore %s\n", version)
		os.Exit(0)
	}
	utils.CheckExclusiveFlags("debug", "quiet", "verbose")
	utils.CheckMandatoryFlags("timestamp")
	if !utils.IsValidTimestamp(*timestamp) {
		logger.Fatal(errors.Errorf("Timestamp %s is invalid.  Timestamps must be in the format YYYYMMDDHHMMSS.", *timestamp), "")
	}
	logger.Info("Restore Key = %s", *timestamp)
}

// This function handles setup that must be done after parsing flags.
func DoSetup() {
	if *quiet {
		logger.SetVerbosity(utils.LOGERROR)
	} else if *debug {
		logger.SetVerbosity(utils.LOGDEBUG)
	} else if *verbose {
		logger.SetVerbosity(utils.LOGVERBOSE)
	}
	connection = utils.NewDBConn("postgres")
	connection.Connect()
	connection.Exec("SET application_name TO 'gprestore'")

	logger.Verbose("Gathering information on dump directories")
	segConfig := utils.GetSegmentConfiguration(connection)
	globalCluster = utils.NewCluster(segConfig, *dumpDir, *timestamp)

	reportFile := utils.MustOpenFileForReading(globalCluster.GetReportFilePath())
	backupReport = utils.ReadReportFile(reportFile)
	utils.EnsureBackupVersionCompatibility(backupReport.BackupVersion, version)
}

func DoRestore() {
	globalCluster.VerifyDirectoriesExistOnAllHosts()
	tableMap := ReadTableMapFile(globalCluster.GetTableMapFilePath())
	backupFileCount := len(tableMap)
	globalCluster.VerifyBackupFileCountOnSegments(backupFileCount)

	masterDumpDir := globalCluster.GetDirForContent(-1)
	globalFilename := fmt.Sprintf("%s/global.sql", masterDumpDir)
	predataFilename := fmt.Sprintf("%s/predata.sql", masterDumpDir)
	postdataFilename := fmt.Sprintf("%s/postdata.sql", masterDumpDir)

	if *restoreGlobals {
		logger.Info("Restoring global database metadata from %s", globalFilename)
		restoreGlobal(globalFilename)
		logger.Info("Global database metadata restore complete")
	}

	connection.Close()
	dbname := backupReport.DatabaseName
	connection = utils.NewDBConn(dbname)
	connection.Connect()
	connection.Exec("SET application_name TO 'gprestore'")

	logger.Info("Restoring pre-data metadata from %s", predataFilename)
	restorePredata(predataFilename)
	logger.Info("Pre-data metadata restore complete")

	logger.Info("Restoring data")
	restoreData(tableMap)
	logger.Info("Data restore complete")

	logger.Info("Restoring post-data metadata from %s", postdataFilename)
	restorePostdata(postdataFilename)
	logger.Info("Post-data metadata restore complete")
}

func restoreGlobal(filename string) {
	utils.ExecuteSQLFile(connection, filename)
}

func restorePredata(filename string) {
	utils.ExecuteSQLFile(connection, filename)
}

func restoreData(tableMap map[string]uint32) {
	for name, oid := range tableMap {
		logger.Verbose("Reading data for table %s from file", name)
		dumpFile := globalCluster.GetTableBackupFilePathForCopyCommand(oid)
		CopyTableIn(connection, name, dumpFile)
	}
}

func restorePostdata(filename string) {
	utils.ExecuteSQLFile(connection, filename)
}

func DoTeardown() {
	if r := recover(); r != nil {
		fmt.Println(r)
	}
	if connection != nil {
		connection.Close()
	}
	// TODO: Add logic for error codes based on whether we Abort()ed or not
}
