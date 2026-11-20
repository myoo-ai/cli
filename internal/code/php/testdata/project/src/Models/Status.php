<?php

namespace App\Models;

enum Status: string
{
    case Active = 'active';
    case Banned = 'banned';

    const DEFAULT = self::Active;

    public function label(): string
    {
        return match ($this) {
            Status::Active => 'Active',
            self::Banned => 'Banned',
        };
    }
}
