<?php

declare(strict_types=1);

namespace App\Models;

use App\Support\HasTimestamps;
use JsonSerializable;

#[\Attribute]
final class User extends Model implements JsonSerializable
{
    use HasTimestamps;

    public string $nickname = '';

    public function __construct(
        public readonly int $id,
        private string $email,
        protected ?Status $status = null,
    ) {
        $this->touch();
    }

    public function table(): string
    {
        return self::TABLE_PREFIX . 'users';
    }

    public function email(): string
    {
        return $this->email;
    }

    public function jsonSerialize(): array
    {
        return ['id' => $this->id, 'status' => $this->status?->label()];
    }

    public string $displayName {
        get => $this->nickname !== '' ? $this->nickname : $this->email;
        set(string $value) {
            $this->nickname = trim($value);
        }
    }
}
