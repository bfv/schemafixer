#!/bin/bash

set -e

echo "==================================="
echo "Project Initialization Script"
echo "==================================="
echo ""

# Prompt for project name
read -p "Enter project name: " project_name
while [ -z "$project_name" ]; do
    echo "Project name cannot be empty."
    read -p "Enter project name: " project_name
done

# Prompt for title
read -p "Enter project title: " title
while [ -z "$title" ]; do
    echo "Title cannot be empty."
    read -p "Enter project title: " title
done

# Prompt for description
read -p "Enter project description: " description
while [ -z "$description" ]; do
    echo "Description cannot be empty."
    read -p "Enter project description: " description
done

echo ""
echo "==================================="
echo "Configuration Summary:"
echo "==================================="
echo "Project Name: $project_name"
echo "Title: $title"
echo "Description: $description"
echo ""
read -p "Proceed with replacing placeholders? (y/n): " confirm

if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
    echo "Aborted."
    exit 1
fi

echo ""
echo "Replacing placeholders in all files..."

# Find all files (excluding .git directory and this script) and replace placeholders
find . -type f -not -path "./.git/*" -not -name "init.sh" | while read -r file; do
    # Skip binary files
    if file "$file" | grep -q "text"; then
        # Check if file contains any placeholders
        if grep -q "<<project_name>>\|<<title>>\|<<description>>" "$file" 2>/dev/null; then
            echo "Processing: $file"
            # Create a temporary file for sed operations
            sed -i.bak \
                -e "s|<<project_name>>|$project_name|g" \
                -e "s|<<title>>|$title|g" \
                -e "s|<<description>>|$description|g" \
                "$file"
            # Remove backup file
            rm -f "${file}.bak"
        fi
    fi
done

echo ""
echo "==================================="
echo "Initialization complete!"
echo "==================================="
echo ""
echo "Next steps:"
echo "1. Review the changes"
echo "2. Set the github repository secrets and variables as needed (e.g. DOCKERHUB_USERNAME, DOCKERHUB_TOKEN)"
echo ""

# Ask user if they want to delete this script
read -p "Delete this initialization script (init.sh)? (y/n): " delete_script

if [ "$delete_script" = "y" ] || [ "$delete_script" = "Y" ]; then
    echo "Deleting init.sh..."
    rm -- "$0"
    echo "Script deleted successfully!"
else
    echo "Script kept. You can delete it manually later if needed."
fi

# Ask user if they want to commit and push
read -p "Commit and push the initialization changes? (y/n): " commit_changes

if [ "$commit_changes" = "y" ] || [ "$commit_changes" = "Y" ]; then
    echo ""
    echo "Staging all changes..."
    git add .
    
    echo "Committing changes..."
    git commit -m "Initialize project: $project_name"
    
    read -p "Push to remote repository? (y/n): " push_changes
    if [ "$push_changes" = "y" ] || [ "$push_changes" = "Y" ]; then
        echo "Pushing to remote..."
        git push
        echo "Changes pushed successfully!"
    else
        echo "Changes committed locally. You can push manually later with: git push"
    fi
else
    echo "Skipping commit. You can commit manually later."
fi

echo "done!"
