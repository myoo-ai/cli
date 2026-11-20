<?php

namespace App\Support;

trait HasTimestamps
{
    public ?int $updatedAt = null;

    public function touch(): void
    {
        $this->updatedAt = time();
    }
}
