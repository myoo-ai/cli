<?php

declare(strict_types=1);

namespace App\Models;

/**
 * Base class of the persisted models.
 */
abstract class Model
{
    protected static array $registry = [];

    public const TABLE_PREFIX = 'app_';

    abstract public function table(): string;

    public static function find(int $id): ?static
    {
        return static::$registry[$id] ?? null;
    }

    public function save(): static
    {
        self::$registry[spl_object_id($this)] = $this;
        return $this;
    }
}
