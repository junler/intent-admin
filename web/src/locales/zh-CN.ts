import english from './en-US';

export default Object.fromEntries(
  Object.keys(english).map((source) => [source, source]),
);
