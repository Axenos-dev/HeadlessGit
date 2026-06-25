-- name: CreateLFSObject :one
insert into lfs_objects (
  user_id, repository_id, object_id, size_bytes
) values (
  ?, ?, ?, ?
) returning *;

-- name: GetLFSObject :one
select * from lfs_objects
where object_id = ? and repository_id=? limit 1;

-- name: DeleteLFSObject :exec
delete from lfs_objects
where object_id = ? and repository_id=?;

-- name: SetLFSObjectVerified :one
update lfs_objects 
set verified=? where object_id=? and repository_id=?
returning *;