---
name: review
description: Perform a code review
---

Perform a thorough code review with the following criteria:

- Critical review: the review must be done in a critical way. You should be skeptical of the code and try to find any issues without, instead of trusting that it is done properly. Comments should not try to find excuses for the code to be the way it is. Point out technical issues in detail and explain them well.
- Bugs and edge cases: reason through the code with different inputs and identify bugs that may be present in normal and edge cases. Pay special attention to concurrent code and spend extra time there as race conditions can be tricky.
- Simplicity: reason through the code and understand how it works, then try to find a simpler implementation (in terms of understanding) of the code.
- Readability: the code should be readable by a human reader. It should flow like English propose and have simple flow. Avoid excessively functional style in procedural code. Avoid deeply nested code. Avoid deep inheritance. Avoid too many levels of abstractions. Be picky about the naming of variables, functions, and classes and make sure they make intuitive sense. Where possible, names should be consistent across modules.
- Performance: see if the code can be more performant without sacrificing readability or simplicity.

Some special review rules to follow:

- Readability
  - In cases a function is only used in a single place without the anticipation that it will be reused and it is not really helping readability of the caller, consider inlining the function directly.
  - If a particular code is surprising or non-trivial, a comment should be left with it to explain the reasoning behind it.

Once the review is completed, the review comments should be in two sections: an overall comment and inline comments.

- The overall comment should summarize the findings and identify the top issues that can cause breakages. It should include an overall "feeling" for how good the code is. It should also include a description of how the code reviewed works. Nitpick comments can be left in the inline comments.
- The inline comments should be organized with file, function, and line number. Each inline comment should be numbered so it can be addressed to (like inline comment #3). Comments should explain why the code is problematic and how to fix it. Suggest code changes if the fix is small, otherwise just describe what needs to be done.

Use git commands to show the diff of the code. Do not edit any code. Make suggestions only.
