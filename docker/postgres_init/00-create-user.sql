-- create goback database and user
create database goback;
create user goback with password 'goback';
grant all privileges on database goback to goback;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO goback;