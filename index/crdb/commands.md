# Create migration
```shell
go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir migrations transaction_tracking
```

# Generate models
```shell
# install fork from https://github.com/optiman/sqlboiler-crdb

sqlboiler crdb --no-tests --wipe
```