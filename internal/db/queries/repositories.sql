-- name: GetRepository :one
select * from repositories
where id=? limit 1;

-- name: ListUserRepositories :many
select * from repositories 
where owner_id=?;

-- name: CreateRepository :one
insert into repositories (
  owner_id, repository_name, storage_path, visibility
) values (
  ?, ?, ?, ?
) returning *;

-- name: DeleteRepository :exec
delete from repositories
where id=?;