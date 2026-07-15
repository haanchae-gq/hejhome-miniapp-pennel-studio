module.exports = {
  extends: ['tuya-panel'],
  rules: {
    camelcase: 0,
    '@typescript-eslint/ban-ts-comment': 0,
    '@typescript-eslint/explicit-module-boundary-types': 0,
  },
  overrides: [
    {
      // `test/` is harness, not product code. The stubs stand in for the Ray
      // runtime and the device SDK, so rules aimed at shipped React components
      // (defaultProps contracts) do not apply, and the stubbed generic
      // signatures must keep type parameters they never use in order to match
      // the real ones.
      files: ['test/**/*.ts', 'test/**/*.tsx'],
      rules: {
        'react/require-default-props': 0,
        'import/no-named-as-default': 0,
        '@typescript-eslint/no-unused-vars': 0,
        '@typescript-eslint/no-var-requires': 0,
        // The `jest/*` rules come from `eslint-config-tuya-panel`, but the suite
        // runs on vitest. Two of them misread it:
        //   - assertions inside a shared helper (`expectTapTarget`) look absent
        //   - vitest's `expect(value, message)` looks like a jest arity error
        'jest/expect-expect': [
          1,
          { assertFunctionNames: ['expect', 'expectTapTarget', 'assertTapTarget'] },
        ],
        'jest/valid-expect': 0,
      },
    },
  ],
};
