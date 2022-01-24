#!/bin/bash

set -ex

# setup cluster and install gpbackup tools using gppkg
ccp_src/scripts/setup_ssh_to_cluster.sh
out=$(ssh -t mdw 'source env.sh && psql postgres -c "select version();"')
GPDB_VERSION=$(echo ${out} | sed -n 's/.*Greenplum Database \([0-9]\).*/\1/p')
mkdir -p /tmp/untarred
tar -xzf gppkgs/gpbackup-gppkgs.tar.gz -C /tmp/untarred
scp /tmp/untarred/gpbackup_tools*gp${GPDB_VERSION}*${OS}*.gppkg mdw:/home/gpadmin
ssh -t mdw "source env.sh; gppkg -i gpbackup_tools*.gppkg"

cat <<SCRIPT > /tmp/run_tests.bash
  #!/bin/bash

  set -ex
  source env.sh

  cat << CONFIG > \${HOME}/s3_config.yaml
executablepath: \${GPHOME}/bin/gpbackup_s3_plugin
options:
  region: ${REGION}
  aws_access_key_id: ${AWS_ACCESS_KEY_ID}
  aws_secret_access_key: ${AWS_SECRET_ACCESS_KEY}
  bucket: ${BUCKET}
  folder: test/backup
  backup_multipart_chunksize: 100MB
  restore_multipart_chunksize: 100MB
CONFIG

cat << CONFIG > \${HOME}/cl_config.yaml
  executablepath: \${GPHOME}/bin/gpbackup_s3_plugin
  options:
    endpoint: ${CL_ENDPOINT}
    aws_access_key_id: ${CL_AWS_ACCESS_KEY_ID}
    aws_secret_access_key: ${CL_AWS_SECRET_ACCESS_KEY}
    bucket: ${CL_BUCKET}
    folder: ${CL_FOLDER}
    backup_multipart_chunksize: 100MB
    restore_multipart_chunksize: 100MB
CONFIG

  pushd ~/go/src/github.com/greenplum-db/gpbackup/plugins
    ./plugin_test.sh \${GPHOME}/bin/gpbackup_s3_plugin \${HOME}/cl_config.yaml
    ./plugin_test.sh \${GPHOME}/bin/gpbackup_s3_plugin \${HOME}/s3_config.yaml
  popd
SCRIPT

chmod +x /tmp/run_tests.bash
scp /tmp/run_tests.bash mdw:/home/gpadmin/run_tests.bash
ssh -t mdw "/home/gpadmin/run_tests.bash"
