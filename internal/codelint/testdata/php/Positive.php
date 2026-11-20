<?php
/**
 * Positive.php - examples of every rule.
 * Shows what code_lint reports.
 */

namespace Fixture;

use RuntimeException;

class Positive
{
    // ---------- Fields ----------
    private int $counter = 0;

    /**
     * This is a method that gets the user.
     */
    public function getUser(string $id): string
    {
        // Step 1: Read it from the environment
        $user = getenv($id);
        // Here we return the user to the caller
        return $user;
    }

    public function count(): int
    {
        // Increment the counter variable here
        $this->counter++;
        return $this->counter;
    }

    public function steps(string $text): string
    {
        // First, trim the whitespace off the text
        $trimmed = trim($text);
        // Next, convert the text to upper case
        return strtoupper($trimmed);
    }

    public function placeholder(): void
    {
        throw new \LogicException('Not implemented');
    }

    public function todo(): void
    {
        // TODO
    }

    public function report(): void
    {
        echo "🔥 hot\n";
        echo "• item\n";
        echo "a — b\n";
        echo str_repeat('=', 40);
        echo "==========";
        $message = sprintf('done');
        printf("no placeholders\n");
    }

    public function swallow(): void
    {
        try {
            $this->risky();
        } catch (\Throwable $e) {
        }
        try {
            $this->risky();
        } catch (\Exception $e) {
            error_log($e->getMessage());
        }
    }

    /** @phpstan-ignore-next-line */
    public function unused(int $x): void
    {
        // phpcs:ignore
        $y = 1;
        /** @psalm-suppress MixedAssignment */
        $z = $x;
    }

    public function chatty(int $x): int
    {
        // This explanation starts here
        // and keeps going
        // for a while
        // without saying much
        // about anything
        // in particular
        return $x;
    }

    public function nested(array $a): int
    {
        foreach ($a as $row) {
            foreach ($row as $cell) {
                if ($cell > 0) {
                    while ($cell > 1) {
                        if ($cell === 2) {
                            return $cell;
                        }
                    }
                }
            }
        }
        return 0;
    }

    public function longFunction(): int
    {
        $total = 0;
        $total += 0;
        $total += 1;
        $total += 2;
        $total += 3;
        $total += 4;
        $total += 5;
        $total += 6;
        $total += 7;
        $total += 8;
        $total += 9;
        $total += 10;
        $total += 11;
        $total += 12;
        $total += 13;
        $total += 14;
        $total += 15;
        $total += 16;
        $total += 17;
        $total += 18;
        $total += 19;
        $total += 20;
        $total += 21;
        $total += 22;
        $total += 23;
        $total += 24;
        $total += 25;
        $total += 26;
        $total += 27;
        $total += 28;
        $total += 29;
        $total += 30;
        $total += 31;
        $total += 32;
        $total += 33;
        $total += 34;
        $total += 35;
        $total += 36;
        $total += 37;
        $total += 38;
        $total += 39;
        $total += 40;
        $total += 41;
        $total += 42;
        $total += 43;
        $total += 44;
        $total += 45;
        $total += 46;
        $total += 47;
        $total += 48;
        $total += 49;
        $total += 50;
        $total += 51;
        $total += 52;
        $total += 53;
        $total += 54;
        $total += 55;
        $total += 56;
        $total += 57;
        $total += 58;
        $total += 59;
        $total += 60;
        $total += 61;
        $total += 62;
        $total += 63;
        $total += 64;
        $total += 65;
        $total += 66;
        $total += 67;
        $total += 68;
        $total += 69;
        $total += 70;
        $total += 71;
        $total += 72;
        $total += 73;
        $total += 74;
        $total += 75;
        $total += 76;
        $total += 77;
        $total += 78;
        $total += 79;
        return $total;
    }

    public function copyOne(array $rows, int $limit): array
    {
        $result = [];
        foreach ($rows as $row) {
            if ($row->size > $limit && str_starts_with($row->name, 'a')) {
                $result[] = $row->name . ':' . $row->size * 2 . ':' . $row->owner;
            } elseif ($row->size < 0) {
                throw new \InvalidArgumentException('negative size ' . $row->name);
            }
        }
        return array_slice($result, 0, $limit);
    }

    public function copyTwo(array $items, int $maximum): array
    {
        $found = [];
        foreach ($items as $item) {
            if ($item->weight > $maximum && str_starts_with($item->label, 'b')) {
                $found[] = $item->label . ':' . $item->weight * 3 . ':' . $item->parent;
            } elseif ($item->weight < 0) {
                throw new \InvalidArgumentException('negative weight ' . $item->label);
            }
        }
        return array_slice($found, 0, $maximum);
    }

    private function risky(): void
    {
    }
}

class LanguageRules
{
    public function rules(array $items, ?object $user, string $name, $callback, array $args): mixed
    {
        $long = array(1, 2, 3);
        $same = $name == $user->name;
        if ($items != $args) {
            return null;
        }
        $has = strpos($name, 'x') !== false;
        $missing = false === strpos($name, 'y');
        $starts = strpos($name, 'ab') === 0;
        $prefix = substr($name, 0, 3) === 'abc';
        $suffix = substr($name, -4) !== '.php';
        $value = isset($items['k']) ? $items['k'] : 'none';
        $name = $name ?? 'anonymous';
        $dir = dirname(__FILE__);
        $class = get_class($this);
        $self = get_class();
        $other = get_class($user);
        $n = sizeof($items);
        $csv = join(',', $items);
        $exists = key_exists('k', $items);
        $trimmed = chop($name);
        $int = is_integer($n) || is_long($n) || is_double($n);
        $sub = strchr($name, '@');
        $first = pos($items);
        $content = @file_get_contents($name);
        var_dump($items);
        print_r($items);
        die(var_dump($user));
        dd($user);
        $email = $user !== null ? $user->email : null;
        $phone = $user === null ? null : $user->phone();
        $result = call_user_func($callback, 1, 2);
        $spread = call_user_func_array($callback, $args);
        list($a, $b) = $items;
        extract($args);
        $fn = create_function('$x', 'return $x;');
        $count = intval($name);
        $isNull = is_null($user);
        array_push($items, $name);
        $square = pow($n + 1, 2);
        switch ($n) {
            case 1:
                return 'one';
            case 2:
                return 'two';
            default:
                return 'many';
        }
    }
}

class Checkout
{
    public function pay(int $total): string
    {
        // The former Cart class computed this, see the old logic in git.
        if ($total < 0) {
            throw new InvalidArgumentException('Invalid input');
        }
        $this->assertTrue(true);
        self::assertSame(1, 1);
        return 'Whoops! Something went wrong.';
    }
}
?>
