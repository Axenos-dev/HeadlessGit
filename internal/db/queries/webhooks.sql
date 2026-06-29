-- name: CreateWebhook :one
insert into webhooks (
  repository_id, secret, url
) values (
  ?, ?, ?
) returning *;

-- name: ListWebhooksForRepository :many
select * from webhooks
where repository_id=?;

-- name: DeleteWebhook :exec
delete from webhooks
where id=?;