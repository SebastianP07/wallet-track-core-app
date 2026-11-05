-- name: ListTags :many
SELECT id, name, color, created_at, updated_at
FROM tags
ORDER BY name, id;

-- name: CreateTag :one
INSERT INTO tags (name, color)
VALUES ($1, $2)
RETURNING id, name, color, created_at, updated_at;

-- name: UpdateTag :one
UPDATE tags
SET name = COALESCE(sqlc.narg(name), name), color = COALESCE(sqlc.narg(color), color)
WHERE id = @id
RETURNING id, name, color, created_at, updated_at;

-- name: DeleteTag :execrows
DELETE FROM tags
WHERE id = $1;
