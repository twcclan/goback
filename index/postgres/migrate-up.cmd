setlocal
FOR /F "tokens=*" %%i in ('type .dbenv') do SET %%i
%MIGRATE% -source file://./migrations -database "%DB_URL%" down 1
%MIGRATE% -source file://./migrations -database "%DB_URL%" up
sqlboiler --wipe psql
endlocal