# GoogleTakeoutFixer

<p align="center">
    <img src="images/GoogleTakeoutFixer.png" alt="drawing" width="200"/>
</p>

A tool that allows you to easily merge Google's weird JSON metadata with your images.

> [!IMPORTANT]
> This project is still in development. Not ready for use yet.

## The Issue
When you download your images from Google's "Google Photos" service through "Google Takeout", the metadata (location, time of creation, etc) is **often saved separately in JSON files instead of being embedded directly into your photos and videos.**
This can lead to problems:
- Files cannot be reliably sorted chronologically or by location
- A cluttered export with a messy file structure and many unnecessary files

## Solution
GoogleTakeoutFixer solves these issues by:
- **Writing EXIF metadata** directly into your media.
- **Organizing your files** into a clear and structured folder structure for easier navigation.
- **Automatically removing unnecessary JSON files**.

## Disclaimer
This project is an **independent open-source project** and is **not affiliated with, endorsed, or sponsored by Google LLC or any of its subsidiaries**. The use of the name "Google" in this repository is solely for descriptive purposes.