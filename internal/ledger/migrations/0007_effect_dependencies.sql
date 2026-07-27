CREATE INDEX IF NOT EXISTS idx_effect_dependencies_parent
ON effect_dependencies(depends_on_effect_id, effect_id);
