#!/bin/sh
while read -r file; do
	echo effect >>"/var/lib/clickhouse/user_files/$file"
	echo 1
done
