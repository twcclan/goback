```shell script
sudo apt-get update

sudo apt-get install \
    apt-transport-https \
    ca-certificates \
    curl \
    gnupg-agent \
    software-properties-common

curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -

sudo add-apt-repository \
   "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
   $(lsb_release -cs) \
   stable"

sudo apt-get update

sudo apt-get install docker-ce docker-ce-cli containerd.io

sudo docker run \
    -d \
    --name postgres \
    -e POSTGRES_PASSWORD=postgres \
    -e POSTGRES_USER=postgres \
    -e POSTGRES_DB=postgres \
    -e PGDATA=/var/lib/postgresql/data/pgdata \
    -v $(pwd)/postgres_db:/var/lib/postgresql/data \
    -p 5432:5432 \
    postgres:12.1

sudo docker run \
    -d \
    --name goback \
    --net host \
    twcclan/goback \
    --index postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable \
    --storage gcs://goback-standard \
    fix

sudo docker run \
    -it \
    --net host \
    --rm \
    -v $(pwd)/dump:/dump \
    --entrypoint pg_dump \
    postgres:12.1 \
    -h localhost \ 
    -p 5432 \
    -U postgres \
    -f /dump/goback.sql \
    postgres

gsutil cp goback.sql gcs://goback-standard/goback.sql


docker-compose run \
    --rm \
    --entrypoint psql \
    -v $(pwd)/dump:/dump \
    postgres \
    -h postgres \
    -p 5432 \
    -U postgres \
    -d goback \
    -f /dump/goback.sql
```