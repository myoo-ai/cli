import React, { useState } from 'react';

export interface ButtonProps {
  label: string;
  onClick?: (count: number) => void;
}

export function Button({ label, onClick }: ButtonProps) {
  const [count, setCount] = useState<number>(0);
  const handle = () => {
    setCount(count + 1);
    onClick?.(count);
  };
  return (
    <button className="btn" data-count={count} onClick={handle} disabled>
      {label} clicked {count} times, isn't it <b>great</b>?
    </button>
  );
}

export const identity = <T,>(value: T): T => value;

export default function Toolbar(props: { items: string[] }) {
  return (
    <>
      {props.items.map((item) => (
        <Button key={item} label={item} />
      ))}
      <Icons.Star size={2 / 1} />
    </>
  );
}

const Icons = { Star: (p: { size: number }) => <svg width={p.size} /> };
