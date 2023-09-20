// SPDX-FileCopyrightText: 2023 The Crossplane Authors <https://crossplane.io>

// SPDX-License-Identifier: CC0-1.0

/** @type {import("prettier").Config} */
const config = {
    overrides: [
        {
            files: ['*.md'],
            options: {
                parser: 'markdown',
                editorconfig: true,
                proseWrap: 'always',
            },
        },
    ],
}

module.exports = config
