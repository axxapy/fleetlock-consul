
command=$1

case $command in
    "consul")
        onsul agent -dev
        ;;
    
    "lock")
        curl -H "fleet-lock-protocol: true" \
                -X POST \
                -d '{"client_params": {"id": "xxxx"}}' \
                127.0.0.1:9090/v1/pre-reboot
        ;;
    
    "unlock")
        curl -H "fleet-lock-protocol: true" \
            -X POST \
            -d '{"client_params": {"id": "xxxx"}}' \
            127.0.0.1:9090/v1/steady-state
        ;;
    *)
        echo "Usage: $0 {consul|lock|unlock}"
        exit 1
        ;;
esac 
