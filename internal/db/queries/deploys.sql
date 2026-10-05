-- name: CreateDeploy :one
INSERT INTO deploys (bot_id, number, source, git_url, git_ref, git_commit, rollback_of, artifact_sha256, artifact_bytes, created_by)
VALUES (@bot_id, (SELECT coalesce(max(number), 0) + 1 FROM deploys d WHERE d.bot_id = @bot_id),
        @source, @git_url, @git_ref, @git_commit, @rollback_of, @artifact_sha256, @artifact_bytes, @created_by)
RETURNING *;

-- name: GetDeploy :one
SELECT * FROM deploys WHERE id = $1;

-- name: ListDeploys :many
SELECT deploys.*, users.name AS created_by_name
FROM deploys LEFT JOIN users ON users.id = deploys.created_by
WHERE bot_id = $1
ORDER BY number DESC
LIMIT 50;

-- name: SetDeployStatus :exec
UPDATE deploys SET status = @status::deploy_status, error = @error,
                   finished_at = CASE WHEN @status::deploy_status IN ('live', 'failed', 'superseded') THEN now() ELSE finished_at END
WHERE id = @id;

-- name: AppendDeployLog :exec
UPDATE deploys SET log = right(log || $2, 200000) WHERE id = $1;

-- name: SupersedePendingDeploys :exec
UPDATE deploys SET status = 'superseded', finished_at = now()
WHERE bot_id = $1 AND id <> $2 AND status IN ('queued', 'fetching', 'installing');

-- name: SupersedeLiveDeploys :exec
UPDATE deploys SET status = 'superseded'
WHERE bot_id = $1 AND id <> $2 AND status = 'live';

-- name: DeployBelongsToNode :one
SELECT deploys.artifact_sha256 FROM deploys
JOIN bots ON bots.id = deploys.bot_id
WHERE deploys.id = $1 AND bots.node_id = $2 AND bots.deleted_at IS NULL;

-- name: LastLiveDeploy :one
SELECT id FROM deploys WHERE bot_id = $1 AND status = 'live' ORDER BY number DESC LIMIT 1;
