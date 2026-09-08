-- name: CreateLFSObject :one
insert into lfs_objects (
  user_id, repository_id, object_id, size_bytes, storage_key
) values (
  ?, ?, ?, ?, ?
) on conflict(repository_id, object_id) do nothing 
returning *;

-- name: CreateVerifiedLFSObject :one
insert into lfs_objects (
  user_id, repository_id, object_id, size_bytes, storage_key, verified
) values (
  ?, ?, ?, ?, ?, 1
) on conflict(repository_id, object_id) do nothing
returning *;

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

-- name: VerifyLFSObjectAtKey :one
update lfs_objects
set user_id=?, size_bytes=?, storage_key=?, verified=1
where object_id=? and repository_id=? and verified=0
returning *;
