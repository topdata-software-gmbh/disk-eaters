#!/bin/bash
#
# DISK EATERS WATCH SCRIPT
# ------------------------
# This script identifies the largest directories and files on your system
# and tracks their growth over time.
#
# Features:
# - Finds the 5 biggest directories
# - Finds the 5 biggest files
# - Tracks growth of directories and files (comparing to previous run)
#
# Usage: 
#   - Run manually: ./disk_eaters.sh [directory_to_scan]
#   - For cron setup, see comments at the end of this script

set -e

# Default values
SCAN_DIR="${1:-/}"
DATE=$(date +"%Y-%m-%d")
LOG_DIR="/var/log/disk_eaters"
HISTORY_DIR="${LOG_DIR}/history"
CURRENT_SNAPSHOT="${LOG_DIR}/current"
PREVIOUS_SNAPSHOT="${LOG_DIR}/previous"
MAX_ITEMS=5

# Create log directories if they don't exist
mkdir -p "${LOG_DIR}" "${HISTORY_DIR}"

# Function to print a header
print_header() {
    echo "=================================================="
    echo "  $1"
    echo "=================================================="
}

# Function to find largest directories
find_largest_directories() {
    print_header "TOP ${MAX_ITEMS} LARGEST DIRECTORIES UNDER ${SCAN_DIR}"
    
    # Use du to find directories, excluding certain system paths
    du -x --max-depth=4 "${SCAN_DIR}" 2>/dev/null | sort -rn | head -n ${MAX_ITEMS} | \
    awk '{
        size = $1; 
        $1=""; 
        sub(/^ /, "", $0);
        
        # Convert size to human-readable format
        if (size >= 1048576) {
            printf "%.2f GB\t%s\n", size/1048576, $0
        } else if (size >= 1024) {
            printf "%.2f MB\t%s\n", size/1024, $0
        } else {
            printf "%d KB\t%s\n", size, $0
        }
    }' | tee -a "${CURRENT_SNAPSHOT}.dirs"
    
    echo ""
}

# Function to find largest files
find_largest_files() {
    print_header "TOP ${MAX_ITEMS} LARGEST FILES UNDER ${SCAN_DIR}"
    
    # Use find to locate files, then sort by size
    find "${SCAN_DIR}" -xdev -type f -exec du -h {} \; 2>/dev/null | \
    sort -rh | head -n ${MAX_ITEMS} | \
    awk '{print $1 "\t" $2}' | tee -a "${CURRENT_SNAPSHOT}.files"
    
    echo ""
}

# Function to compare current data with previous run and identify growth
analyze_growth() {
    local type=$1  # "dirs" or "files"
    local entity=$2  # "DIRECTORIES" or "FILES"
    
    print_header "TOP ${MAX_ITEMS} FASTEST GROWING ${entity} UNDER ${SCAN_DIR}"
    
    if [[ -f "${PREVIOUS_SNAPSHOT}.${type}" ]]; then
        # Extract paths from current snapshot
        awk '{$1=""; sub(/^ /, "", $0); print $0}' "${CURRENT_SNAPSHOT}.${type}" > "${CURRENT_SNAPSHOT}.${type}.paths"
        
        # Create a mapping of paths to sizes for both current and previous snapshots
        awk '{size=$1; $1=""; sub(/^ /, "", $0); gsub(/[A-Z]/, "", size); gsub(/\./, "", size); print $0 "\t" size}' "${CURRENT_SNAPSHOT}.${type}" > "${CURRENT_SNAPSHOT}.${type}.map"
        awk '{size=$1; $1=""; sub(/^ /, "", $0); gsub(/[A-Z]/, "", size); gsub(/\./, "", size); print $0 "\t" size}' "${PREVIOUS_SNAPSHOT}.${type}" > "${PREVIOUS_SNAPSHOT}.${type}.map"
        
        # Find growth rates using join
        join -a1 -t $'\t' "${CURRENT_SNAPSHOT}.${type}.map" "${PREVIOUS_SNAPSHOT}.${type}.map" | \
        awk -F'\t' '{
            if (NF == 3) {
                growth = $2 - $3;
                if (growth > 0) {
                    # Convert growth to human-readable format
                    if (growth >= 1048576) {
                        hr_growth = sprintf("%.2f GB", growth/1048576);
                    } else if (growth >= 1024) {
                        hr_growth = sprintf("%.2f MB", growth/1024);
                    } else {
                        hr_growth = sprintf("%d KB", growth);
                    }
                    printf "%s\t%s\n", hr_growth, $1;
                }
            }
        }' | sort -rh | head -n ${MAX_ITEMS}
        
        # Clean up temporary files
        rm -f "${CURRENT_SNAPSHOT}.${type}.paths" "${CURRENT_SNAPSHOT}.${type}.map" "${PREVIOUS_SNAPSHOT}.${type}.map"
    else
        echo "No previous data available for comparison. Growth analysis will be available after the next run."
    fi
    
    echo ""
}

# Main execution

# Create a new result file with timestamp
RESULT_FILE="${HISTORY_DIR}/disk_eaters_${DATE}.log"
echo "DISK EATERS WATCH REPORT - ${DATE}" > "${RESULT_FILE}"
echo "Scan Directory: ${SCAN_DIR}" >> "${RESULT_FILE}"
echo "" >> "${RESULT_FILE}"

# Redirect output to both console and log file
exec > >(tee -a "${RESULT_FILE}") 2>&1

# Run the analysis
find_largest_directories
find_largest_files

# Check for previous data to analyze growth
if [[ -f "${PREVIOUS_SNAPSHOT}.dirs" && -f "${PREVIOUS_SNAPSHOT}.files" ]]; then
    analyze_growth "dirs" "DIRECTORIES"
    analyze_growth "files" "FILES"
else
    print_header "GROWTH ANALYSIS"
    echo "No previous data available for comparison. Growth analysis will be available after the next run."
    echo ""
fi

# Archive current data for next run's comparison
if [[ -f "${CURRENT_SNAPSHOT}.dirs" && -f "${CURRENT_SNAPSHOT}.files" ]]; then
    cp "${CURRENT_SNAPSHOT}.dirs" "${PREVIOUS_SNAPSHOT}.dirs"
    cp "${CURRENT_SNAPSHOT}.files" "${PREVIOUS_SNAPSHOT}.files"
fi

print_header "SUMMARY"
echo "Log saved to: ${RESULT_FILE}"
echo "Run this script daily to track growth patterns."
echo ""

# Cleanup current snapshot files (we've already saved them as "previous")
rm -f "${CURRENT_SNAPSHOT}.dirs" "${CURRENT_SNAPSHOT}.files"

# Add cron setup instructions at the end of the log
cat << 'EOF' >> "${RESULT_FILE}"

--------------------------------------------------------
CRON SETUP INSTRUCTIONS
--------------------------------------------------------
To run this script daily via cron, execute:

sudo crontab -e

Then add the following line:

# Run disk eaters watch script daily at 2 AM
0 2 * * * /path/to/disk_eaters.sh / > /dev/null 2>&1

Replace "/path/to/" with the actual path where you saved this script.
Replace "/" with the directory you want to scan if not the root.
EOF

exit 0