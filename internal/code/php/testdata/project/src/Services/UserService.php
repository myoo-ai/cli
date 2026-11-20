<?php

declare(strict_types=1);

namespace App\Services;

use App\Models\{User, Status as State};
use App\Models\Model;
use function App\Support\format_price;
use const App\Support\CURRENCY;

class UserService
{
    private array $users = [];

    public function __construct(private readonly \Closure $clock)
    {
    }

    public function register(int $id, string $email): User
    {
        $user = new User($id, $email, State::Active);
        $user->save()->table();
        $this->users[$id] = $user;
        # A hash comment, not an attribute.
        $count = count($this->users);
        $log = function (string $msg) use (&$count, $user): string {
            $count++;
            return "{$user->email()}: $msg #$count";
        };
        $double = fn(int $x): int => $x * 2 + $count;
        [$first, [$second]] = [$log('created'), [$double(2)]];
        list('a' => $third) = ['a' => (int) $second];
        return $user;
    }

    public function report(): string
    {
        global $config;
        $total = 0;
        foreach ($this->users as $key => $user) {
            $total += $user->id;
        }
        $label = format_price($total, CURRENCY);
        $text = <<<EOT
            Users: {$total} $label
            First: {$this->users[0]->nickname}
            EOT;
        $raw = <<<'RAW'
            No $interpolation here
            RAW;
        try {
            $found = Model::find($total);
        } catch (\RuntimeException | \LogicException $e) {
            return $e->getMessage();
        }
        return $text . $raw . ($config ?? '') . compact('found', 'label')['label'];
    }
}
