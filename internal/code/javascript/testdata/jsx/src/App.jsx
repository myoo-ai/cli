import Toolbar, { Button as Btn } from './Button';

/**
 * Root component.
 */
export function App({ user }) {
  const title = user ? <h1>Hello {user.name}</h1> : null;
  const ratio = 10 / 2 / 1;
  return (
    <main>
      {title}
      {/* comment child */}
      <Toolbar items={['a', 'b']} />
      <Btn label={`ratio ${ratio}`} onClick={() => alert(ratio)}>
        <span>{'</'}not a tag</span>
      </Btn>
    </main>
  );
}
